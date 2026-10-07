package overworld

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	java26 "github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/world"
)

type bonusFeatureSource struct {
	ModernFeatureSource
	terrain string
	states  map[string]world.NativeState
}

func (s bonusFeatureSource) BaseState(x, y, z int32) (uint32, uint32) {
	name := "air"
	if y >= 0 && y < 32 && s.terrain != "empty" {
		switch {
		case s.terrain == "ceiling" || y < 4:
			name = "stone"
		case y == 4:
			name = "stone"
			if s.terrain == "fence" {
				name = "oak_fence"
			}
		case y == 5:
			switch s.terrain {
			case "leaves":
				name = "oak_leaves"
			case "leaf_hole":
				if x&15 != 8 || z&15 != 8 {
					name = "oak_leaves"
				}
			case "flowers":
				name = "dandelion"
			case "water":
				name = "water"
			}
		}
	}
	state := s.states[name]
	return state.Block, state.Liquid
}
func TestBonusChestMatchesJava263(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_bonus_chest.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Terrain   string
		Seed      int64
		Xoroshiro bool
		Origin    [3]int32
		Placed    bool
		Next      int64
		Blocks    []struct {
			Position [3]int32
			State    any
		}
		Actors []struct {
			Position [3]int32
			Loot     string
			Seed     int64
		}
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	states := map[string]world.NativeState{}
	for _, name := range []string{"air", "stone", "oak_fence", "oak_leaves", "dandelion", "water"} {
		state, err := world.ResolveState(world.DefaultBlockRegistry, world.BlockState{Name: "minecraft:" + name})
		if err != nil {
			t.Fatal(err)
		}
		states[name] = state
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s/%d/xor=%v/%v", tc.Terrain, tc.Seed, tc.Xoroshiro, tc.Origin), func(t *testing.T) {
			region := countLayerTestRegion()
			source := bonusFeatureSource{terrain: tc.Terrain, states: states}
			region.f.source = source
			origin := featurePos{tc.Origin[0], tc.Origin[1], tc.Origin[2]}
			region.target = world.ChunkPos{origin.x >> 4, origin.z >> 4}
			region.SetOrigin(region.target[0], region.target[1])
			for x := uint8(0); x < 16; x++ {
				for z := uint8(0); z < 16; z++ {
					for y := int16(0); y < 32; y++ {
						primary, liquid := source.BaseState(region.target[0]*16+int32(x), int32(y), region.target[1]*16+int32(z))
						region.c.SetBlock(x, y, z, 0, primary)
						region.c.SetBlock(x, y, z, 1, liquid)
					}
				}
			}
			random := java26.NewLegacyRandom(tc.Seed)
			if tc.Xoroshiro {
				random = java26.NewXoroshiroRandom(tc.Seed)
			}
			if got := region.f.placeConfiguredData(region, "bonus_chest", nil, origin, &random); got != tc.Placed {
				t.Errorf("placed=%v, Java wants %v", got, tc.Placed)
			}
			if next := random.NextLong(); next != tc.Next {
				t.Errorf("following stream=%d, Java wants %d", next, tc.Next)
			}
			if len(region.overlay) != len(tc.Blocks) {
				t.Errorf("writes=%d, Java wants %d", len(region.overlay), len(tc.Blocks))
			}
			for _, input := range tc.Blocks {
				p := featurePos{input.Position[0], input.Position[1], input.Position[2]}
				block, ok := featureStateBlock(input.State)
				if !ok {
					t.Fatal("invalid source state")
				}
				want, err := block.ResolveNative(region.c.BlockRegistry())
				if err != nil {
					t.Fatal(err)
				}
				if got, ok := region.overlay[p]; !ok || got != want {
					t.Errorf("at %v: got %+v present=%v, source projection %+v", p, got, ok, want)
				}
			}
			if len(region.result.BlockEntities) != len(tc.Actors) {
				t.Fatalf("actors=%d, Java wants %d", len(region.result.BlockEntities), len(tc.Actors))
			}
			for i, want := range tc.Actors {
				got := region.result.BlockEntities[i]
				if got.Pos[0] != int(want.Position[0]) || got.Pos[1] != int(want.Position[1]) || got.Pos[2] != int(want.Position[2]) || got.Data["LootTable"] != want.Loot || got.Data["LootTableSeed"] != want.Seed {
					t.Errorf("actor=%+v, source=%+v", got, want)
				}
			}
		})
	}
}
