package overworld

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

func TestBuiltinSurvivalMatchesJava263(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_survival.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name     string
		State    any
		Survives bool
		Blocks   []struct {
			Position [3]int32
			State    any
		}
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			region := countLayerTestRegion()
			origin := featurePos{8, 10, 8}
			for _, block := range tc.Blocks {
				state, ok := featureStateBlock(block.State)
				if !ok {
					t.Fatal("invalid fixture state")
				}
				p := featurePos{origin.x + block.Position[0], origin.y + block.Position[1], origin.z + block.Position[2]}
				native, err := state.ResolveNative(region.c.BlockRegistry())
				if err != nil {
					t.Fatal(err)
				}
				region.SetNativeBlock(p, native, state.NativeBlock)
			}
			predicate := map[string]any{"type": "would_survive", "state": tc.State}
			if got := featurePredicate(region, origin, predicate); got != tc.Survives {
				t.Errorf("survives=%v, executed Java wants %v", got, tc.Survives)
			}
		})
	}
}

func TestMultifaceRejectsOccupiedFace(t *testing.T) {
	region := countLayerTestRegion()
	p := featurePos{8, 10, 8}
	lichen, err := world.ResolveState(region.c.BlockRegistry(), world.BlockState{Name: "minecraft:glow_lichen", Properties: map[string]any{"east": true}})
	if err != nil {
		t.Fatal(err)
	}
	region.Set(p, lichen)
	region.Set(multifaceOffset(p, "east"), countLayerNativeState(t, "stone"))
	region.Set(multifaceOffset(p, "west"), countLayerNativeState(t, "stone"))
	random := java26.NewLegacyRandom(0)
	if multifacePlaceAt(region, "glow_lichen", "minecraft:stone", p, &random, []string{"east", "west"}, 0) {
		t.Fatal("occupied first matching face must reject placement, not add the next face")
	}
	_, properties := region.State(p)
	if properties["west"] != false {
		t.Fatalf("rejected placement changed west face: %v", properties)
	}
}

func TestMultifaceUsesSourceAttachmentShape(t *testing.T) {
	region := countLayerTestRegion()
	p := featurePos{8, 10, 8}
	stair, err := world.ResolveState(region.c.BlockRegistry(), world.BlockState{Name: "minecraft:oak_stairs", Properties: map[string]any{"facing": "north", "half": "bottom", "shape": "inner_left"}})
	if err != nil {
		t.Fatal(err)
	}
	region.Set(p, stair)
	if !multifaceCanAttach(region, p, "west") {
		t.Fatal("inner-left stair must support its complete west face")
	}
	if multifaceCanAttach(region, p, "east") {
		t.Fatal("inner-left stair must not support its partial east face")
	}
}

type multifacePostprocessGenerator struct{}

func (multifacePostprocessGenerator) DefaultSpawn(world.Dimension) cube.Pos {
	return cube.Pos{8, 10, 8}
}
func (multifacePostprocessGenerator) GenerateChunk(pos world.ChunkPos, c *chunk.Chunk, _ world.GenerationContext) world.GenerationResult {
	var result world.GenerationResult
	if pos != (world.ChunkPos{}) {
		return result
	}
	region := newFeatureRegion(&ModernFeatures{}, c, pos, &result)
	p := featurePos{8, 10, 8}
	stone, err := world.ResolveState(c.BlockRegistry(), world.BlockState{Name: "minecraft:stone"})
	if err != nil {
		panic(err)
	}
	air, err := world.ResolveState(c.BlockRegistry(), world.BlockState{Name: "minecraft:air"})
	if err != nil {
		panic(err)
	}
	region.Set(multifaceOffset(p, "east"), stone)
	if !multifacePlaceFace(region, "glow_lichen", p, "east") {
		panic("lichen fixture placement failed")
	}
	// A later decoration removes the support before the generated chunk becomes live.
	region.Set(multifaceOffset(p, "east"), air)
	return result
}

func TestMultifaceSpreadReceivesGenerationPostprocessing(t *testing.T) {
	w := world.Config{Synchronous: true, Generator: multifacePostprocessGenerator{}, RandomTickSpeed: -1}.New()
	defer w.Close()
	p := cube.Pos{8, 10, 8}
	check := func(tx *world.Tx, want string) {
		id, err := tx.JavaBlockStateID(p)
		if err != nil {
			t.Fatal(err)
		}
		state, err := world.JavaState(world.DefaultBlockRegistry, world.NativeState{JavaStateID: id})
		if err != nil {
			t.Fatal(err)
		}
		if state.Name != want {
			t.Fatalf("lichen lifecycle state = %s, want %s", state.Name, want)
		}
	}
	<-w.Exec(func(tx *world.Tx) {
		check(tx, "minecraft:glow_lichen")
		for dx := range 3 {
			for dz := range 3 {
				tx.Block(cube.Pos{(dx - 1) * 16, 10, (dz - 1) * 16})
			}
		}
	})
	w.AdvanceTick()
	<-w.Exec(func(tx *world.Tx) { check(tx, "minecraft:air") })
}
