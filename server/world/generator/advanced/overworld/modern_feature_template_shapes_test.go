package overworld

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/structure"
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
			template := structure.Template{Size: [3]int32{1, 1, 1}, Palettes: [][]structure.Block{{{State: world.BlockState{Name: placed}}}}}
			placement, err := template.Place(0, [3]int32{pos.x, pos.y, pos.z}, structure.None, structure.NoMirror, structure.Bounds{Min: [3]int32{0, 0, 0}, Max: [3]int32{15, 255, 15}}, 17)
			if err != nil {
				t.Fatal(err)
			}
			placement.TemplateUpdateFlags = 3
			placement.TemplateKnownShape = false
			region.f.applyStructurePlacement(region, placement)
			got, err := world.CanonicalState(world.DefaultBlockRegistry, region.nativeState(check))
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
			template := structure.Template{
				Size:     [3]int32{1, 1, 1},
				Palettes: [][]structure.Block{{{State: world.BlockState{Name: "minecraft:iron_bars"}}}},
			}
			placement, err := template.Place(0, [3]int32{pos.x, pos.y, pos.z}, structure.None, structure.NoMirror, structure.Bounds{Min: [3]int32{0, 0, 0}, Max: [3]int32{15, 255, 15}}, 17)
			if err != nil {
				t.Fatal(err)
			}
			placement.TemplateUpdateFlags = tc.flags
			placement.TemplateKnownShape = tc.knownShape
			region.f.applyStructurePlacement(region, placement)
			got, err := world.CanonicalState(world.DefaultBlockRegistry, region.nativeState(pos))
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
	template := structure.Template{
		Size: [3]int32{1, 1, 1},
		Palettes: [][]structure.Block{{
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
	placement, err := template.Place(0, pos, structure.None, structure.NoMirror,
		structure.Bounds{Min: [3]int32{0, 0, 0}, Max: [3]int32{15, 255, 15}}, 17)
	if err != nil {
		t.Fatal(err)
	}
	placement.TemplateUpdateFlags = 3
	placement.TemplateKnownShape = false
	region.f.applyStructurePlacement(region, placement)

	state, err := world.CanonicalState(world.DefaultBlockRegistry, region.nativeState(featurePos{pos[0], pos[1], pos[2]}))
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

func TestTreeLeafDistancePreservesSourceStaleBucketWrites(t *testing.T) {
	region := templateFluidTestRegion()
	logs := []featurePos{{8, 64, 8}, {9, 66, 8}}
	near, stale := featurePos{9, 65, 8}, featurePos{9, 64, 8}
	for pos, name := range map[featurePos]string{
		logs[0]: "minecraft:oak_log",
		logs[1]: "minecraft:oak_log",
		near:    "minecraft:oak_leaves",
		stale:   "minecraft:oak_leaves",
	} {
		block, ok := featureStateBlock(name)
		if !ok {
			t.Fatalf("could not resolve feature block %s", name)
		}
		state, err := block.ResolveNative(region.c.BlockRegistry())
		if err != nil {
			t.Fatal(err)
		}
		region.Set(pos, state)
	}
	updateModernLeafDistance(region, logs, []featurePos{near, stale}, nil, nil)
	for pos, want := range map[featurePos]int32{near: 1, stale: 2} {
		state, err := world.CanonicalState(region.c.BlockRegistry(), region.nativeState(pos))
		if err != nil {
			t.Fatal(err)
		}
		if state.Name != "minecraft:oak_leaves" || state.Properties["distance"] != want {
			t.Errorf("leaf at %v = %v, want Java queue result distance %d", pos, state, want)
		}
	}
}

func TestBlobFoliageCornerRowsMatchReference(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_blob_corner_rows.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Xoroshiro bool       `json:"xoroshiro"`
		Seed      int64      `json:"seed"`
		Positions [][3]int32 `json:"positions"`
		NextLong  int64      `json:"next_long"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		region := templateFluidTestRegion()
		center := featurePos{8, 64, 8}
		random := vanilla.NewLegacyRandom(tc.Seed)
		if tc.Xoroshiro {
			random = vanilla.NewXoroshiroRandom(tc.Seed)
		}
		provider := map[string]any{"id": "minecraft:oak_leaves"}
		placed := make(map[featurePos]bool)
		treeFoliageRow(region, "blob_foliage_placer", nil, provider, center, 1, 0, false, &random, placed, nil)
		treeFoliageRow(region, "blob_foliage_placer", nil, provider, center, 1, -1, false, &random, placed, nil)
		wantLeaves := make(map[featurePos]bool, len(tc.Positions))
		for _, offset := range tc.Positions {
			wantLeaves[treePos(center, offset[0], offset[1], offset[2])] = true
		}
		for _, y := range []int32{0, -1} {
			for dx := int32(-1); dx <= 1; dx++ {
				for dz := int32(-1); dz <= 1; dz++ {
					pos := treePos(center, dx, y, dz)
					state, err := world.CanonicalState(region.c.BlockRegistry(), region.nativeState(pos))
					if err != nil {
						t.Fatal(err)
					}
					want := "minecraft:air"
					if wantLeaves[pos] {
						want = "minecraft:oak_leaves"
					}
					if state.Name != want {
						t.Errorf("xoroshiro=%v foliage at %v=%s, Java=%s", tc.Xoroshiro, pos, state.Name, want)
					}
				}
			}
		}
		if got := random.NextLong(); got != tc.NextLong {
			t.Errorf("xoroshiro=%v following RNG=%d, Java=%d", tc.Xoroshiro, got, tc.NextLong)
		}
	}
}
