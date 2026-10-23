package overworld

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/structure"
)

type templateFeatureSource struct {
	ModernFeatureSource
	wet               bool
	air, stone, water world.NativeState
}

func (s templateFeatureSource) BaseState(x, y, z int32) world.NativeState {
	state := s.air
	if y == 79 {
		state = s.stone
	} else if y == 80 && s.wet {
		state = s.water
	}
	return state
}
func (s templateFeatureSource) BaseBlock(x, y, z int32) world.Block {
	rid := s.BaseState(x, y, z).Block
	b, _ := world.DefaultBlockRegistry.BlockByRuntimeID(rid)
	return b
}
func (s templateFeatureSource) BaseHeight(int32, int32, bool) int32 { return 80 }
func (s templateFeatureSource) BiomeAt(int32, int32, int32) string  { return "desert" }
func TestTemplateFeatureMatchesReference(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_templates.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Template, Terrain            string
		Seed, NextLong               int64
		Placed, Xoroshiro, Processed bool
		Writes                       []struct {
			Position [3]int32
			State    any
		}
		Actors []struct {
			X, Y, Z       int
			LootTable     string
			LootTableSeed int64
		}
	}
	if err = json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	native := func(name string) world.NativeState {
		s, err := world.ResolveState(world.DefaultBlockRegistry, world.BlockState{Name: "minecraft:" + name})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	well := requireJSON("configured_feature", "desert_well")
	processors := well["features"].([]any)[1].(map[string]any)["feature"].(map[string]any)["processors"]
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s/%s/%d/xor=%v/processed=%v", tc.Template, tc.Terrain, tc.Seed, tc.Xoroshiro, tc.Processed), func(t *testing.T) {
			source := templateFeatureSource{ModernFeatureSource: modernTerrainSource{}, wet: tc.Terrain == "wet", air: native("air"), stone: native("sandstone"), water: native("water")}
			c := chunk.New(world.DefaultBlockRegistry, cube.Range{0, 255})
			region := newFeatureRegion(&ModernFeatures{source: source}, c, world.ChunkPos{}, &world.GenerationResult{})
			for x := uint8(0); x < 16; x++ {
				for z := uint8(0); z < 16; z++ {
					for y := int16(79); y <= 80; y++ {
						world.SetNativeState(c, x, y, z, source.BaseState(int32(x), int32(y), int32(z)))
					}
				}
			}
			random := vanilla.NewLegacyRandom(tc.Seed)
			if tc.Xoroshiro {
				random = vanilla.NewXoroshiroRandom(tc.Seed)
			}
			config := map[string]any{"templates": []any{map[string]any{"weight": float64(1), "data": map[string]any{"id": tc.Template}}}}
			if tc.Processed {
				config["processors"] = processors
			}
			if placed := placeModernTemplateFeature(region, config, featurePos{8, 80, 8}, &random); placed != tc.Placed {
				t.Errorf("placed=%v source=%v", placed, tc.Placed)
			}
			if next := random.NextLong(); next != tc.NextLong {
				t.Errorf("following RNG=%d source=%d", next, tc.NextLong)
			}
			if region.overlay.len() != len(tc.Writes) {
				t.Errorf("written positions=%d source=%d", region.overlay.len(), len(tc.Writes))
			}
			for _, v := range tc.Writes {
				b, ok := featureStateBlock(v.State)
				if !ok {
					t.Fatal("invalid source state")
				}
				want, err := b.ResolveNative(c.BlockRegistry())
				if err != nil {
					t.Fatal(err)
				}
				p := featurePos{v.Position[0], v.Position[1], v.Position[2]}
				if got, ok := region.overlay.get(p); !ok || got != want {
					t.Errorf("at %v got=%v present=%v source=%v", p, got, ok, want)
				}
			}
			if tc.Processed {
				if len(region.result.BlockEntities) != len(tc.Actors) {
					t.Fatalf("actors=%d source=%d", len(region.result.BlockEntities), len(tc.Actors))
				}
				for i, actor := range tc.Actors {
					got := region.result.BlockEntities[i]
					if got.Pos != (cube.Pos{actor.X, actor.Y, actor.Z}) || got.Data["LootTable"] != actor.LootTable || got.Data["LootTableSeed"] != actor.LootTableSeed {
						t.Errorf("actor=%+v source=%+v", got, actor)
					}
				}
			}
		})
	}
}

func TestTemplateRuleObservesPriorFeature(t *testing.T) {
	native := func(name string) world.NativeState {
		state, err := world.ResolveState(world.DefaultBlockRegistry, world.BlockState{Name: "minecraft:" + name})
		if err != nil {
			t.Fatal(err)
		}
		return state
	}
	source := templateFeatureSource{air: native("air"), stone: native("sandstone"), water: native("water")}
	c := chunk.New(world.DefaultBlockRegistry, cube.Range{0, 255})
	region := newFeatureRegion(&ModernFeatures{source: source}, c, world.ChunkPos{}, &world.GenerationResult{})
	random := vanilla.NewLegacyRandom(123)
	origin := featurePos{8, 80, 8}
	if !region.PlaceValue(map[string]any{"type": "simple_block", "to_place": map[string]any{"id": "minecraft:stone"}}, origin, &random) {
		t.Fatal("preceding stone feature failed")
	}
	config := map[string]any{
		"type":      "minecraft:template",
		"templates": []any{map[string]any{"weight": float64(1), "data": map[string]any{"id": "minecraft:desert_well/suspicious_sand"}}},
		"processors": map[string]any{"processors": []any{map[string]any{
			"processor_type": "minecraft:rule",
			"rules": []any{map[string]any{
				"input_predicate":    map[string]any{"predicate_type": "minecraft:always_true"},
				"location_predicate": map[string]any{"predicate_type": "minecraft:block_match", "block": "minecraft:stone"},
				"output_state":       "minecraft:gold_block",
			}},
		}}},
	}
	if !region.PlaceValue(config, origin, &random) {
		t.Fatal("template placement failed")
	}
	if got := region.nativeState(origin); got != native("gold_block") {
		t.Fatalf("at %v got=%v, want rule output replacing the earlier feature", origin, got)
	}
}

func TestLavaSubmergedStructurePublishesOnlyFinalBlockActors(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	lavaProcessor, err := structure.LoadProcessors(nil, []structure.ProcessorSpec{{Type: "lava_submerged_block"}}, structure.ProcessorEnvironment{
		BaseBlock: func(x, _, _ int32) world.Block {
			if x == 8 {
				return block.Lava{Depth: 8, Still: true}
			}
			return block.Stone{}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	chest := world.BlockState{Name: "minecraft:chest", Properties: map[string]any{
		"facing": "north", "type": "single", "waterlogged": false,
	}}
	template := structure.Template{
		Size: [3]int32{2, 1, 1},
		Palettes: [][]structure.Block{{
			{
				Pos:   [3]int32{0, 0, 0},
				State: chest,
				NBT: map[string]any{
					"id": "minecraft:chest", "LootTable": "minecraft:chests/ruined_portal", "LootTableSeed": int64(1),
				},
			},
			{
				Pos:   [3]int32{1, 0, 0},
				State: chest,
				NBT: map[string]any{
					"id": "minecraft:chest", "LootTable": "minecraft:chests/ruined_portal", "LootTableSeed": int64(2),
				},
			},
		}},
	}
	placement, err := template.Place(0, [3]int32{8, 64, 8}, structure.None, structure.NoMirror,
		structure.Bounds{Min: [3]int32{0, 0, 0}, Max: [3]int32{15, 127, 15}}, 0, lavaProcessor...)
	if err != nil {
		t.Fatal(err)
	}
	if len(placement.Blocks) != 2 || placement.Blocks[0].State.Name != "minecraft:lava" ||
		placement.Blocks[0].NBT["id"] != "minecraft:chest" {
		t.Fatalf("lava processor output = %#v, want lava with preserved source chest NBT", placement.Blocks)
	}

	c := chunk.New(world.DefaultBlockRegistry, cube.Range{0, 127})
	result := &world.GenerationResult{}
	region := newFeatureRegion(&ModernFeatures{}, c, world.ChunkPos{}, result)
	region.SetOrigin(0, 0)
	(&ModernFeatures{}).applyStructurePlacement(region, placement)

	lava, _ := world.DefaultBlockRegistry.BlockByRuntimeID(c.Block(8, 64, 8, 0))
	if name, _ := lava.EncodeBlock(); name != "minecraft:lava" {
		t.Fatalf("final submerged block = %s, want lava", name)
	}
	chestBlock, _ := world.DefaultBlockRegistry.BlockByRuntimeID(c.Block(9, 64, 8, 0))
	if name, _ := chestBlock.EncodeBlock(); name != "minecraft:chest" {
		t.Fatalf("final chest block = %s, want chest", name)
	}
	if len(result.BlockEntities) != 1 {
		t.Fatalf("published block entities = %v, want only the surviving chest actor", result.BlockEntities)
	}
	actor := result.BlockEntities[0]
	if actor.Pos != (cube.Pos{9, 64, 8}) || actor.Data["id"] != "Chest" ||
		actor.Data["LootTable"] != "minecraft:chests/ruined_portal" || actor.Data["LootTableSeed"] != int64(2) {
		t.Fatalf("published actor = %+v, want the final chest's loot payload", actor)
	}
}
