package world

import (
	"encoding/json"
	"math/bits"
	"os"
	"reflect"
	"sort"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
)

type capturedAreaWord struct {
	Value         uint32
	Bound         int
	Sequence      int64
	Caller        string
	BoundedCaller string `json:"bounded_caller"`
}

type nativeAreaReplayRegion struct {
	centre ChunkPos
	radius int
	circle bool
}

func TestPlayerAreaDispatcherMatchesOwnedBDSOverlap(t *testing.T) {
	replayNativeChunkAreaDispatcher(t, "bedrock_1_26_51_overlapping_chunk_areas.json", 45, 15,
		[]nativeAreaReplayRegion{{centre: ChunkPos{0, 0}, radius: 1}, {centre: ChunkPos{2, 0}, radius: 1}})
}

func TestPlayerAreaDispatcherMatchesOwnedBDSCircle(t *testing.T) {
	for _, c := range []struct {
		file          string
		radius, count int
	}{
		{"bedrock_1_26_51_circle2_chunk_area.json", 2, 25},
		{"bedrock_1_26_51_circle4_chunk_area.json", 4, 81},
	} {
		t.Run(c.file, func(t *testing.T) {
			replayNativeChunkAreaDispatcher(t, c.file, 20, c.count, []nativeAreaReplayRegion{{radius: c.radius, circle: true}})
		})
	}
}

func replayNativeChunkAreaDispatcher(t *testing.T, file string, frames, chunkCount int, regions []nativeAreaReplayRegion) {
	t.Helper()
	data, err := os.ReadFile("../block/testdata/" + file)
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete    bool
		Checkpoints []struct {
			Tick  int64
			Areas []struct {
				Tick      int64
				Randomize bool
				Bounds    []int32
				Circle    *bool `json:"is_circle"`
			}
			Chunks []struct {
				Position ChunkPos
				Words    []capturedAreaWord
			}
			Outside []capturedAreaWord
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Checkpoints) != frames {
		t.Fatal("incomplete owned-BDS overlapping area capture")
	}
	random := &areaVisitRandom{}
	w := Config{Synchronous: true, RandomTickSpeed: -1, RandSource: random}.New()
	defer w.Close()
	loaders := make([]*Loader, len(regions))
	for i, region := range regions {
		loaders[i] = NewLoader(region.radius, w, NopViewer{})
	}
	<-w.Exec(func(tx *Tx) {
		defer func() {
			for _, loader := range loaders {
				loader.Close(tx)
			}
		}()
		minimum, maximum := regions[0].centre, regions[0].centre
		for i, region := range regions {
			r := int32(region.radius)
			minimum[0] = min(minimum[0], region.centre[0]-r-1)
			minimum[1] = min(minimum[1], region.centre[1]-r-1)
			maximum[0] = max(maximum[0], region.centre[0]+r+1)
			maximum[1] = max(maximum[1], region.centre[1]+r+1)
			loaders[i].pos = region.centre
			loaders[i].forceActive = true
			loaders[i].square = !region.circle
		}
		for x := minimum[0]; x <= maximum[0]; x++ {
			for z := minimum[1]; z <= maximum[1]; z++ {
				tx.chunk(ChunkPos{x, z})
			}
		}
		var order []ChunkPos
		var clock int64
		visited := map[ChunkPos]bool{}
		// Eligibility comes from the independently observed native visits,
		// including circular-area corners. It is not selected by Go's filter.
		for _, chunk := range native.Checkpoints[1].Chunks {
			pos := chunk.Position
			col := tx.chunk(pos)
			hot := false
			for _, region := range regions {
				if pos == region.centre {
					hot = true
				}
			}
			// Each native hot chunk has a hopper, barrel and brewer. Only
			// the empty hopper consumes two extra words on even ticks.
			// A zero-draw one-actor witness observes each cold chunk without
			// adding a shuffle draw or simulating native block contents.
			count := 1
			if hot {
				count = 3
			}
			for i := 0; i < count; i++ {
				draws := hot && i == 0
				actor := &actorIdentityTicker{}
				actor.callback = func(tx *Tx) {
					if !visited[pos] {
						visited[pos] = true
						order = append(order, pos)
					}
					if draws && clock%2 == 0 {
						tx.RandomUint32()
						tx.RandomUint32()
					}
				}
				col.updateBlockEntity(cube.Pos{int(pos[0])*16 + i, 4, int(pos[1]) * 16}, actor)
			}
			w.addActiveColumn(pos, col)
		}
		w.conf.Synchronous = false
		defer func() { w.conf.Synchronous = true; random.active = false }()
		for _, frame := range native.Checkpoints[1:] {
			if len(frame.Areas) != len(regions) || len(frame.Chunks) != chunkCount {
				t.Fatal("native area/chunk controls differ")
			}
			for i, area := range frame.Areas {
				if area.Tick != frame.Tick || area.Randomize != randomizeChunkArea(frame.Tick, uint64(i)) {
					t.Fatalf("tick %d area %d native phase differs", frame.Tick, i)
				}
				if area.Circle != nil && *area.Circle != regions[i].circle {
					t.Fatal("native area shape control differs")
				}
				if len(area.Bounds) != 0 {
					region := regions[i]
					r := int32(region.radius)
					want := []int32{region.centre[0] - r, 0, region.centre[1] - r, region.centre[0] + r, 0, region.centre[1] + r, r*2 + 1, 1, r*2 + 1}
					if len(area.Bounds) != 12 || !reflect.DeepEqual(area.Bounds[:9], want) {
						t.Fatal("native child-view bounds control differs")
					}
				}
			}
			events := []capturedAreaWord{}
			for _, word := range frame.Outside {
				if word.Bound == 255 {
					if word.BoundedCaller != "0x581e87e" && word.BoundedCaller != "0x581e895" {
						t.Fatal("unexpected native area bounded caller")
					}
					events = append(events, word)
				}
			}
			var want []ChunkPos
			for _, chunk := range frame.Chunks {
				want = append(want, chunk.Position)
				for _, word := range chunk.Words {
					switch word.Caller {
					case "0x9fd6b3", "0x57f82d6", "0x57f82f5":
					default:
						t.Fatal("unexpected chunk actor RNG caller")
					}
					events = append(events, word)
				}
			}
			sort.Slice(events, func(i, j int) bool { return events[i].Sequence < events[j].Sequence })
			random.words, random.used = random.words[:0], 0
			for _, word := range events {
				value := uint64(word.Value)<<32 | 8
				if word.Bound == 255 {
					// Go's explicit rand.Source uses multiply-high bounded
					// reduction. Map the independently observed native modulo
					// result to the middle of its Go input interval. Raw actor
					// words are unchanged; no expected output selects an input.
					lower, _ := bits.Div64(uint64(word.Value%255), 0, 255)
					value = lower + (^uint64(0)/255)/2
				}
				random.words = append(random.words, value)
			}
			clock, order = frame.Tick, order[:0]
			clear(visited)
			random.active = true
			ticker{}.tickBlocksRandomly(tx, loaders, frame.Tick)
			random.active = false
			if random.used != len(random.words) || !reflect.DeepEqual(order, want) {
				t.Fatalf("tick %d chunk order %v, native %v; draws %d/%d", frame.Tick, order, want, random.used, len(random.words))
			}
		}
	})
}
