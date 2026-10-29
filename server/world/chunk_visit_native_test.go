package world

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
)

func TestChunkRectangleVisitsMatchExecutedNative(t *testing.T) {
	data, err := os.ReadFile("../block/testdata/bedrock_1_26_51_chunk_visits.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete bool
		Circles  []struct {
			Origin, Offset ChunkPos
			Radius         int32
			Inside         bool
		}
		Phases []struct {
			Tick, Index uint64
			Randomize   bool
		}
		Steps     []struct{ State, Next uint8 }
		Scenarios []struct {
			Width, Depth int32
			Words        []uint32
			Positions    []ChunkPos
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Steps) != 255 || len(native.Scenarios) != 60 || len(native.Phases) != 2115 || len(native.Circles) != 2240 {
		t.Fatal("incomplete executed chunk visit capture")
	}
	for i, c := range native.Circles {
		if nativeChunkCircleContains(int64(c.Offset[0]), int64(c.Offset[1]), c.Radius) != c.Inside {
			t.Fatalf("circle case %d differs from client", i)
		}
		pos := ChunkPos{c.Origin[0] + c.Offset[0], c.Origin[1] + c.Offset[1]}
		want := c.Inside && c.Offset[0] >= -c.Radius && c.Offset[0] <= c.Radius && c.Offset[1] >= -c.Radius && c.Offset[1] <= c.Radius
		if blockColumnWithinArea(pos, loaderActiveArea{pos: c.Origin, radius: c.Radius}) != want {
			t.Fatalf("circle case %d chunk bounds differ", i)
		}
	}
	for _, c := range native.Phases {
		if randomizeChunkArea(int64(c.Tick), c.Index) != c.Randomize {
			t.Fatalf("area index %d tick %d phase differs from client", c.Index, c.Tick)
		}
	}
	for _, s := range native.Steps {
		if got := nextChunkVisitState(s.State); got != s.Next {
			t.Fatalf("state %d: next %d, client %d", s.State, got, s.Next)
		}
	}
	for i, c := range native.Scenarios {
		used := 0
		var got []ChunkPos
		bounded := func(bound int) int {
			if bound != 255 || used >= len(c.Words) {
				t.Fatal("unexpected bounded draw")
			}
			word := c.Words[used]
			used++
			return int(word % uint32(bound))
		}
		if !visitChunkRectangle(ChunkPos{}, ChunkPos{c.Width - 1, c.Depth - 1}, true, bounded, func(p ChunkPos) { got = append(got, p) }) || used != 2 || !reflect.DeepEqual(got, c.Positions) {
			t.Fatalf("rectangle %d %dx%d: visits differ (draws=%d)", i, c.Width, c.Depth, used)
		}
	}
}

func TestChunkRectangleOrderMatchesOwnedBDS(t *testing.T) {
	data, err := os.ReadFile("../block/testdata/bedrock_1_26_51_piston_chunk_actor_visits.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete    bool
		Checkpoints []struct {
			Tick    int64
			Chunks  []struct{ Position ChunkPos }
			Outside []struct {
				Value  uint32
				Bound  int
				Caller string `json:"bounded_caller"`
			}
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Checkpoints) != 100 {
		t.Fatal("incomplete native chunk actor trace")
	}
	shuffled := 0
	for _, frame := range native.Checkpoints[1:] {
		words := []uint32{}
		for _, word := range frame.Outside {
			if word.Bound != 255 {
				continue
			}
			if word.Caller != "0x581e87e" && word.Caller != "0x581e895" {
				t.Fatal("unexpected bounded-255 caller")
			}
			words = append(words, word.Value)
		}
		used := 0
		bounded := func(bound int) int {
			if bound != 255 || used >= len(words) {
				t.Fatal("unobserved area traversal draw")
			}
			word := words[used]
			used++
			return int(word % 255)
		}
		var got, want []ChunkPos
		visitChunkRectangle(ChunkPos{-1, -1}, ChunkPos{1, 1}, frame.Tick%20 == 0, bounded, func(pos ChunkPos) { got = append(got, pos) })
		for _, c := range frame.Chunks {
			want = append(want, c.Position)
		}
		if used != len(words) || !reflect.DeepEqual(got, want) {
			t.Fatalf("tick %d order %v, native %v (draws %d/%d)", frame.Tick, got, want, used, len(words))
		}
		if frame.Tick%20 == 0 {
			shuffled++
		}
	}
	if shuffled != 5 {
		t.Fatalf("observed %d shuffled area ticks, want five", shuffled)
	}
}

func TestNativeCircularBlockAreaIncludesScheduledCornerTicks(t *testing.T) {
	// The original-BDS circle4 capture visits (4,4) in the full chunk pass,
	// including its pending block/fluid queues. A radius-squared-16 circle
	// incorrectly excluded that corner while the area's bounding box includes it.
	w := Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	<-w.Exec(func(tx *Tx) {
		for x := int32(3); x <= 6; x++ {
			for z := int32(3); z <= 5; z++ {
				tx.chunk(ChunkPos{x, z})
			}
		}
		area := loaderActiveArea{radius: 4, radiusSq: 16}
		w.conf.Synchronous = false
		defer func() { w.conf.Synchronous = true }()
		if !w.scheduledTickChunkEligible(ChunkPos{4, 4}, []loaderActiveArea{area}) {
			t.Fatal("native circular-area corner excluded from scheduled block/fluid ticks")
		}
		if w.scheduledTickChunkEligible(ChunkPos{5, 4}, []loaderActiveArea{area}) {
			t.Fatal("scheduled ticks escaped the native area bounds")
		}
	})
}

type areaVisitRandom struct {
	words  []uint64
	used   int
	active bool
}

func (r *areaVisitRandom) Uint64() uint64 {
	if !r.active {
		return 8
	}
	if r.used == len(r.words) {
		panic("unobserved area random draw")
	}
	word := r.words[r.used]
	r.used++
	return word
}

func TestAreaActorsRunBeforeTheNextAreaSelectsItsOrder(t *testing.T) {
	for _, overlap := range []bool{false, true} {
		t.Run(fmt.Sprint("overlap_", overlap), func(t *testing.T) {
			random := &areaVisitRandom{words: []uint64{0xabcdef0100000008, 8, 8}}
			if !overlap {
				random.words = append(random.words, 0xabcdef0200000008)
			}
			w := Config{Synchronous: true, RandomTickSpeed: -1, RandSource: random}.New()
			defer w.Close()
			first, second := NewLoader(1, w, NopViewer{}), NewLoader(1, w, NopViewer{})
			var got []uint32
			<-w.Exec(func(tx *Tx) {
				defer first.Close(tx)
				defer second.Close(tx)
				// Build ready dependency neighbourhoods synchronously. Drive
				// only the ordinary player-area dispatcher below; no real-time
				// background ticker or test-specific production hook is used.
				for x := int32(-2); x <= 6; x++ {
					for z := int32(-2); z <= 2; z++ {
						tx.chunk(ChunkPos{x, z})
					}
				}
				centres := []ChunkPos{{0, 0}, {4, 0}}
				if overlap {
					centres[1] = centres[0]
				}
				for i, loader := range []*Loader{first, second} {
					loader.pos, loader.forceActive, loader.square = centres[i], true, true
					if overlap && i == 1 {
						continue
					}
					actor := &actorIdentityTicker{}
					actor.callback = func(tx *Tx) { got = append(got, tx.RandomUint32()) }
					col := tx.chunk(centres[i])
					col.updateBlockEntity(cube.Pos{int(centres[i][0]) * 16, 4, 0}, actor)
					w.addActiveColumn(centres[i], col)
				}
				random.active = true
				w.conf.Synchronous = false
				defer func() { w.conf.Synchronous = true; random.active = false }()
				ticker{}.tickBlocksRandomly(tx, []*Loader{first, second}, 21)
			})
			want := []uint32{0xabcdef01}
			if !overlap {
				want = append(want, 0xabcdef02)
			}
			if !reflect.DeepEqual(got, want) || random.used != len(random.words) {
				t.Fatalf("actor words %v, want %v; draws=%d/%d", got, want, random.used, len(random.words))
			}
		})
	}
}
