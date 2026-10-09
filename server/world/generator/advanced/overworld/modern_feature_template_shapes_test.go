package overworld

import (
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/javastructure"
)

func TestTemplatePlacementUpdatesBoundaryShape(t *testing.T) {
	for _, placedBars := range []bool{true, false} {
		name := "placed bars connect to existing stone"
		if !placedBars {
			name = "existing bars connect to placed stone"
		}
		t.Run(name, func(t *testing.T) {
			region := templateFluidTestRegion()
			pos := featurePos{8, 64, 8}
			east := featurePos{9, 64, 8}
			placed, existing := "minecraft:iron_bars", "minecraft:stone"
			check, property := pos, "east"
			if !placedBars {
				placed, existing = existing, placed
				check, property = east, "west"
			}
			state, err := world.ResolveState(world.DefaultBlockRegistry, world.BlockState{Name: existing})
			if err != nil {
				t.Fatal(err)
			}
			region.Set(east, state)
			template := javastructure.Template{Size: [3]int32{1, 1, 1}, Palettes: [][]javastructure.Block{{{State: world.BlockState{Name: placed}}}}}
			placement, err := template.Place(0, [3]int32{pos.x, pos.y, pos.z}, javastructure.None, javastructure.NoMirror, javastructure.Bounds{Min: [3]int32{0, 0, 0}, Max: [3]int32{15, 255, 15}}, 17)
			if err != nil {
				t.Fatal(err)
			}
			placement.TemplateUpdateFlags = 3
			placement.TemplateKnownShape = false
			region.f.applyStructurePlacement(region, placement)
			got, err := world.JavaState(world.DefaultBlockRegistry, region.nativeState(check))
			if err != nil {
				t.Fatal(err)
			}
			if got.Name != "minecraft:iron_bars" || got.Properties[property] != true {
				t.Errorf("immediate template boundary state=%v, want iron bars with %s=true", got, property)
			}
		})
	}
}
func TestTemplateKnownShapeIsIndependentOfUpdateFlags(t *testing.T) {
	for _, tc := range []struct {
		name       string
		flags      int
		knownShape bool
		wantEast   bool
	}{
		{name: "known-shape flags2 skips", flags: 2, knownShape: true, wantEast: false},
		{name: "unknown-shape flags18 updates", flags: 18, knownShape: false, wantEast: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			region := templateFluidTestRegion()
			pos, east := featurePos{8, 64, 8}, featurePos{9, 64, 8}
			state, err := world.ResolveState(world.DefaultBlockRegistry, world.BlockState{Name: "minecraft:stone"})
			if err != nil {
				t.Fatal(err)
			}
			region.Set(east, state)
			template := javastructure.Template{
				Size:     [3]int32{1, 1, 1},
				Palettes: [][]javastructure.Block{{{State: world.BlockState{Name: "minecraft:iron_bars"}}}},
			}
			placement, err := template.Place(0, [3]int32{pos.x, pos.y, pos.z}, javastructure.None, javastructure.NoMirror, javastructure.Bounds{Min: [3]int32{0, 0, 0}, Max: [3]int32{15, 255, 15}}, 17)
			if err != nil {
				t.Fatal(err)
			}
			placement.TemplateUpdateFlags = tc.flags
			placement.TemplateKnownShape = tc.knownShape
			region.f.applyStructurePlacement(region, placement)
			got, err := world.JavaState(world.DefaultBlockRegistry, region.nativeState(pos))
			if err != nil {
				t.Fatal(err)
			}
			if got.Name != "minecraft:iron_bars" || got.Properties["east"] != tc.wantEast {
				t.Fatalf("placement state=%v, want iron bars with east=%v", got, tc.wantEast)
			}
		})
	}
}
func TestTemplateChestShapeRefreshesInventoryPayload(t *testing.T) {
	region := templateFluidTestRegion()
	pos := [3]int32{8, 64, 8}
	template := javastructure.Template{
		Size: [3]int32{1, 1, 1},
		Palettes: [][]javastructure.Block{{
			{
				State: world.BlockState{Name: "minecraft:chest", Properties: map[string]any{
					"facing": "north", "type": "left", "waterlogged": false,
				}},
				NBT: map[string]any{
					"id": "minecraft:chest", "x": pos[0], "y": pos[1], "z": pos[2],
					"pairx": int32(pos[0] + 1), "pairz": pos[2],
					"Items": []any{map[string]any{"id": "minecraft:diamond", "count": int32(1), "Slot": int8(0)}},
				},
			},
		}},
	}
	placement, err := template.Place(0, pos, javastructure.None, javastructure.NoMirror,
		javastructure.Bounds{Min: [3]int32{0, 0, 0}, Max: [3]int32{15, 255, 15}}, 17)
	if err != nil {
		t.Fatal(err)
	}
	placement.TemplateUpdateFlags = 3
	placement.TemplateKnownShape = false
	region.f.applyStructurePlacement(region, placement)

	state, err := world.JavaState(world.DefaultBlockRegistry, region.nativeState(featurePos{pos[0], pos[1], pos[2]}))
	if err != nil {
		t.Fatal(err)
	}
	if state.Name != "minecraft:chest" || state.Properties["type"] != "single" {
		t.Fatalf("unpaired chest state=%v, want single chest", state)
	}
	for _, actor := range region.result.BlockEntities {
		if actor.Pos[0] != int(pos[0]) || actor.Pos[1] != int(pos[1]) || actor.Pos[2] != int(pos[2]) {
			continue
		}
		items, ok := actor.Data["Items"].([]any)
		if !ok || len(items) != 1 {
			t.Fatalf("refreshed chest lost inventory: %#v", actor.Data["Items"])
		}
		if _, exists := actor.Data["pairx"]; exists {
			t.Fatalf("single chest retained stale pairx metadata: %#v", actor.Data)
		}
		if _, exists := actor.Data["pairz"]; exists {
			t.Fatalf("single chest retained stale pairz metadata: %#v", actor.Data)
		}
		return
	}
	t.Fatal("chest block-entity payload was not published")
}
