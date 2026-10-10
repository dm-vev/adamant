package overworld

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	java26 "github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/javadata"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/javastructure"
)

type strongholdRenderFixture struct {
	Meta struct {
		Box        [6]int32 `json:"box"`
		Dir        string   `json:"dir"`
		Door       string   `json:"door"`
		Kind       string   `json:"kind"`
		LeftChild  bool     `json:"leftChild"`
		RightChild bool     `json:"rightChild"`
		LeftLow    bool     `json:"leftLow"`
		LeftHigh   bool     `json:"leftHigh"`
		RightLow   bool     `json:"rightLow"`
		RightHigh  bool     `json:"rightHigh"`
		Variant    int      `json:"variant"`
		Steps      int      `json:"steps"`
	} `json:"meta"`
	Seed          int64                   `json:"seed"`
	Split         bool                    `json:"split"`
	EventCount    int                     `json:"event_count"`
	EventsSHA256  string                  `json:"events_sha256"`
	Actors        []strongholdRenderActor `json:"actors"`
	Post          [][3]int32              `json:"post"`
	ChestPlaced   bool                    `json:"chest_placed"`
	SpawnerPlaced bool                    `json:"spawner_placed"`
	NextLong      int64                   `json:"next_long"`
}

type strongholdRenderActor struct {
	Pos           [3]int32 `json:"pos"`
	ID            string   `json:"id"`
	LootTable     string   `json:"loot_table"`
	LootTableSeed int64    `json:"loot_table_seed"`
	SpawnEntity   string   `json:"spawn_entity"`
}

func TestStrongholdRenderingMatchesJava263(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_stronghold_render.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []strongholdRenderFixture
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for i, fixture := range fixtures {
		fixture := fixture
		t.Run(fmt.Sprintf("%03d_%s_%s_%s_split_%t", i, fixture.Meta.Kind, fixture.Meta.Dir, fixture.Meta.Door, fixture.Split), func(t *testing.T) {
			m := fixture.Meta
			p := strongholdPiece{
				box:  mbox(m.Box[0], m.Box[1], m.Box[2], m.Box[3], m.Box[4], m.Box[5]),
				kind: m.Kind, dir: m.Dir,
				leftLow: m.LeftChild || m.LeftLow, leftHigh: m.LeftHigh,
				rightLow: m.RightChild || m.RightLow, rightHigh: m.RightHigh,
				variant: m.Variant,
			}
			switch m.Door {
			case "WOOD_DOOR":
				p.door = "wood"
			case "GRATES":
				p.door = "grates"
			case "IRON_DOOR":
				p.door = "iron"
			default:
				p.door = "opening"
			}
			if p.kind == "filler" {
				p.variant = m.Steps
			}
			random := java26.NewLegacyRandom(fixture.Seed)
			air, err := javadata.BlockStateID("minecraft:air", nil)
			if err != nil {
				t.Fatal(err)
			}
			state := javastructure.StructurePieceState{}
			env := javastructure.ProcessorEnvironment{
				JavaBlockStateID: func(int32, int32, int32) (uint32, error) { return air, nil },
				Range:            cube.Range{-64, 320}, Random: &random, PieceState: &state,
			}
			full := javastructure.Bounds{Min: p.box.Min, Max: p.box.Max}
			clips := []javastructure.Bounds{full}
			if fixture.Split {
				mid := full.Min[0] + (full.Max[0]-full.Min[0])/2
				clips = []javastructure.Bounds{
					{Min: full.Min, Max: [3]int32{mid, full.Max[1], full.Max[2]}},
					{Min: [3]int32{mid + 1, full.Min[1], full.Min[2]}, Max: full.Max},
				}
			}
			var blocks []javastructure.Block
			var post [][3]int32
			for _, clip := range clips {
				placement, err := p.Place(clip, env)
				if err != nil {
					t.Fatal(err)
				}
				blocks = append(blocks, placement.Blocks...)
				for _, pos := range placement.PostProcessing {
					post = append(post, [3]int32{int32(pos[0]), int32(pos[1]), int32(pos[2])})
				}
			}
			if got, want := strongholdOrderedPostMarkers(post), strongholdOrderedPostMarkers(fixture.Post); !equalStrongholdMarkers(got, want) {
				t.Errorf("ordered Java post-processing markers = %v, want %v", got, want)
			}
			events := make([]string, len(blocks))
			actors := make([]strongholdRenderActor, 0, len(fixture.Actors))
			for i, block := range blocks {
				keys := make([]string, 0, len(block.State.Properties))
				for key := range block.State.Properties {
					keys = append(keys, key)
				}
				sort.Strings(keys)
				var properties strings.Builder
				for j, key := range keys {
					if j > 0 {
						properties.WriteByte(',')
					}
					fmt.Fprintf(&properties, "%s=%v", key, block.State.Properties[key])
				}
				events[i] = fmt.Sprintf("%d,%d,%d=%s[%s]", block.Pos[0], block.Pos[1], block.Pos[2], block.State.Name, properties.String())
				if block.NBT == nil {
					continue
				}
				actor := strongholdRenderActor{Pos: block.Pos}
				actor.ID, _ = block.NBT["id"].(string)
				actor.LootTable, _ = block.NBT["LootTable"].(string)
				actor.LootTableSeed, _ = block.NBT["LootTableSeed"].(int64)
				if spawn, ok := block.NBT["SpawnData"].(map[string]any); ok {
					if entity, ok := spawn["entity"].(map[string]any); ok {
						actor.SpawnEntity, _ = entity["id"].(string)
					}
				}
				actors = append(actors, actor)
			}
			if len(events) != fixture.EventCount {
				t.Errorf("ordered write count = %d, want %d", len(events), fixture.EventCount)
			}
			encoded, err := json.Marshal(events)
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(encoded)
			if got := hex.EncodeToString(sum[:]); got != fixture.EventsSHA256 {
				t.Errorf("ordered Java write SHA-256 = %s, want %s", got, fixture.EventsSHA256)
			}
			sort.Slice(actors, func(i, j int) bool {
				for axis := range actors[i].Pos {
					if actors[i].Pos[axis] != actors[j].Pos[axis] {
						return actors[i].Pos[axis] < actors[j].Pos[axis]
					}
				}
				return false
			})
			if got, err := json.Marshal(actors); err != nil {
				t.Fatal(err)
			} else if want, err := json.Marshal(fixture.Actors); err != nil {
				t.Fatal(err)
			} else if string(got) != string(want) {
				t.Errorf("block actor payloads = %s, want %s", got, want)
			}
			if state.ChestPlaced != fixture.ChestPlaced || state.SpawnerPlaced != fixture.SpawnerPlaced {
				t.Errorf("warm source piece state = {ChestPlaced:%t SpawnerPlaced:%t}, want {ChestPlaced:%t SpawnerPlaced:%t}", state.ChestPlaced, state.SpawnerPlaced, fixture.ChestPlaced, fixture.SpawnerPlaced)
			}
			if got := random.NextLong(); got != fixture.NextLong {
				t.Errorf("postProcess Java RNG nextLong = %d, want %d", got, fixture.NextLong)
			}
		})
	}
}
func strongholdOrderedPostMarkers(markers [][3]int32) [][3]int32 {
	ordered := append([][3]int32(nil), markers...)
	sort.SliceStable(ordered, func(i, j int) bool {
		a, b := ordered[i], ordered[j]
		if a[0]>>4 != b[0]>>4 {
			return a[0]>>4 < b[0]>>4
		}
		if a[2]>>4 != b[2]>>4 {
			return a[2]>>4 < b[2]>>4
		}
		return (a[1]+64)>>4 < (b[1]+64)>>4
	})
	return ordered
}

func equalStrongholdMarkers(a, b [][3]int32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
