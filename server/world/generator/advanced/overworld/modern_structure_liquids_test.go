package overworld

import (
	"strings"
	"testing"

	java26 "github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/javastructure"
)

type liquidStructureSource struct{ pillarAirSource }

func (liquidStructureSource) JavaNoiseBiomeAt(int32, int32, int32) string { return "minecraft:plains" }

func TestTrialChamberLiquidPolicyReplacesAmbientWater(t *testing.T) {
	for _, policy := range []string{"ignore_waterlogging", "apply_waterlogging"} {
		t.Run(policy, func(t *testing.T) {
			config := map[string]any{}
			for key, value := range requireJavaJSON("structure", "trial_chambers") {
				config[key] = value
			}
			config["liquid_settings"] = policy
			config["size"] = float64(0)
			config["start_height"] = map[string]any{"absolute": float64(80)}
			random := java26.NewLegacyRandom(0)
			source := liquidStructureSource{pillarAirSource{ModernFeatureSource: modernTerrainSource{}, air: world.DefaultBlockRegistry.AirRuntimeID()}}
			start, ok, err := buildModernJigsawStructure(modernStructureBuildInput{seed: 0, name: "trial_chambers", config: config, source: source, random: &random})
			if err != nil || !ok {
				t.Fatalf("trial start: found=%v err=%v", ok, err)
			}
			piece := start.pieces[0].(modernJigsawPiece).piece
			clip := javastructure.Bounds{Min: piece.Bounds.Min, Max: piece.Bounds.Max}
			placed, err := piece.PlaceWithEnvironment(0, clip, 0, javastructure.ProcessorEnvironment{Random: &random, SkipEntities: true})
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range placed.Blocks {
				if !strings.HasSuffix(entry.State.Name, "_slab") || entry.State.Properties["type"] == "double" || entry.State.Properties["waterlogged"] == true {
					continue
				}
				pos := featurePos{entry.Pos[0], entry.Pos[1], entry.Pos[2]}
				selected, err := piece.PlaceWithEnvironment(0, javastructure.Bounds{Min: entry.Pos, Max: entry.Pos}, 0, javastructure.ProcessorEnvironment{Random: &random, SkipEntities: true})
				if err != nil {
					t.Fatal(err)
				}
				if len(selected.Blocks) != 1 {
					t.Fatalf("selected slab clip contains %d blocks", len(selected.Blocks))
				}
				region := templateFluidTestRegion()
				region.target = world.ChunkPos{pos.x >> 4, pos.z >> 4}
				region.SetOrigin(pos.x>>4, pos.z>>4)
				water, err := world.ResolveState(world.DefaultBlockRegistry, world.BlockState{Name: "minecraft:water"})
				if err != nil {
					t.Fatal(err)
				}
				region.Set(pos, water)
				selected.TemplateUpdateFlags, selected.TemplateKnownShape = 18, true
				region.f.applyStructurePlacement(region, selected)
				state, err := world.JavaState(world.DefaultBlockRegistry, region.nativeState(pos))
				if err != nil {
					t.Fatal(err)
				}
				wantWet := policy == "apply_waterlogging"
				if state.Properties["waterlogged"] != wantWet {
					t.Fatalf("trial slab %s at %v: waterlogged=%v, want %v for %s", state.Name, pos, state.Properties["waterlogged"], wantWet, policy)
				}
				if !wantWet && len(region.result.ScheduledTicks) != 0 {
					t.Fatalf("ignored ambient water scheduled updates: %+v", region.result.ScheduledTicks)
				}
				return
			}
			t.Fatal("trial start has no dry single slab to exercise ambient water replacement")
		})
	}
}

func TestListTemplateLiquidPassesCompleteBeforeNextTemplate(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		firstWater, ignoreSecond bool
		wantWet                  bool
	}{
		{"previous template source is no longer locked", true, false, true},
		{"later ignore template replaces refilled slab", false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			region := templateFluidTestRegion()
			slab := world.BlockState{Name: "minecraft:oak_slab", Properties: map[string]any{"type": "bottom", "waterlogged": false}}
			first := javastructure.Block{State: slab}
			if tc.firstWater {
				first.Pos = [3]int32{1, 0, 0}
				first.State = world.BlockState{Name: "minecraft:water"}
			} else {
				water, err := world.ResolveState(world.DefaultBlockRegistry, world.BlockState{Name: "minecraft:water"})
				if err != nil {
					t.Fatal(err)
				}
				region.Set(featurePos{9, 80, 8}, water)
			}
			combined := javastructure.Placement{TemplateUpdateFlags: 18, TemplateKnownShape: true}
			for i, entry := range []javastructure.Block{first, {State: slab}} {
				settings := javastructure.ApplyWaterlogging
				if i == 1 && tc.ignoreSecond {
					settings = javastructure.IgnoreWaterlogging
				}
				piece := javastructure.JigsawPiece{
					Template: &javastructure.Template{Size: [3]int32{2, 1, 1}, Palettes: [][]javastructure.Block{{entry}}},
					Origin:   [3]int32{8, 80, 8}, LiquidSettings: settings,
				}
				placed, err := piece.Place(0, javastructure.Bounds{Min: [3]int32{0, 0, 0}, Max: [3]int32{15, 255, 15}}, 0)
				if err != nil {
					t.Fatal(err)
				}
				combined.Blocks = append(combined.Blocks, placed.Blocks...)
			}
			region.f.applyStructurePlacement(region, combined)
			state, err := world.JavaState(world.DefaultBlockRegistry, region.nativeState(featurePos{8, 80, 8}))
			if err != nil {
				t.Fatal(err)
			}
			if state.Properties["waterlogged"] != tc.wantWet {
				t.Fatalf("final slab waterlogged=%v, want %v", state.Properties["waterlogged"], tc.wantWet)
			}
			if len(region.result.ScheduledTicks) != 1 {
				t.Fatalf("first successful fill must retain one water tick: %+v", region.result.ScheduledTicks)
			}
		})
	}
}

func TestListTemplateReplacementRemovesEarlierActor(t *testing.T) {
	region := templateFluidTestRegion()
	combined := javastructure.Placement{TemplateUpdateFlags: 18, TemplateKnownShape: true}
	for _, entry := range []javastructure.Block{
		{State: world.BlockState{Name: "minecraft:chest"}, NBT: map[string]any{"id": "minecraft:chest", "LootTable": "minecraft:chests/simple_dungeon"}},
		{State: world.BlockState{Name: "minecraft:stone"}},
	} {
		piece := javastructure.JigsawPiece{
			Template: &javastructure.Template{Size: [3]int32{1, 1, 1}, Palettes: [][]javastructure.Block{{entry}}},
			Origin:   [3]int32{8, 80, 8},
		}
		placed, err := piece.Place(0, javastructure.Bounds{Min: [3]int32{0, 0, 0}, Max: [3]int32{15, 255, 15}}, 0)
		if err != nil {
			t.Fatal(err)
		}
		combined.Blocks = append(combined.Blocks, placed.Blocks...)
	}
	region.f.applyStructurePlacement(region, combined)
	name, _ := region.State(featurePos{8, 80, 8})
	if name != "minecraft:stone" {
		t.Fatalf("later template left %s instead of stone", name)
	}
	if len(region.result.BlockEntities) != 0 {
		t.Fatalf("overwritten chest retained a published actor: %+v", region.result.BlockEntities)
	}
}
