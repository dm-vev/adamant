package overworld

import (
	"encoding/json"
	"fmt"
	java26 "github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/world"
	"os"
	"testing"
)

type sculkFeatureSource struct {
	ModernFeatureSource
	terrain string
	states  map[string]world.NativeState
}

func (s sculkFeatureSource) BaseState(x, y, z int32) (uint32, uint32) {
	name := "air"
	if y >= 0 && y < 32 && s.terrain != "empty" {
		switch {
		case s.terrain == "blocked" && x == 8 && y == 8 && z == 8:
			name = "stone"
		case s.terrain == "fence" && x == 8 && y == 7 && z == 8:
			name = "oak_fence"
		case s.terrain == "snow" && x == 8 && y == 7 && z == 8:
			name = "snow"
		case s.terrain == "sculk_start" && x == 8 && y == 8 && z == 8:
			name = "sculk"
		case s.terrain == "vein_start" && x == 8 && y == 8 && z == 8:
			name = "sculk_vein"
		case s.terrain == "flowing" && x == 8 && y == 8 && z == 8:
			name = "flowing"
		case s.terrain == "flooded" && y == 8:
			name = "water"
		case y == 8 && (x != 8 || z != 8):
			switch s.terrain {
			case "grass":
				name = "short_grass"
			case "flowers":
				name = "dandelion"
			case "fire":
				name = "fire"
			}
		case y == 7:
			name = "stone"
		case s.terrain == "wet" && x == 8 && y == 8 && z == 8:
			name = "water"
		}
	}
	state := s.states[name]
	return state.Block, state.Liquid
}
func TestSculkPatchMatchesJava263(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_sculk.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name, Terrain                                         string
		Seed, NextLong                                        int64
		Charges, Amount, Attempts, GrowthRounds, SpreadRounds int
		Placed, Xoroshiro                                     bool
		Writes                                                []struct {
			Position [3]int32
			State    any
		}
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	states := map[string]world.NativeState{}
	for _, name := range []string{"air", "stone", "oak_fence", "water", "sculk", "short_grass", "dandelion", "fire"} {
		state, err := world.ResolveState(world.DefaultBlockRegistry, world.BlockState{Name: "minecraft:" + name})
		if err != nil {
			t.Fatal(err)
		}
		states[name] = state
	}
	for name, source := range map[string]world.BlockState{
		"snow":       {Name: "minecraft:snow", Properties: map[string]any{"layers": int32(8)}},
		"sculk_vein": {Name: "minecraft:sculk_vein", Properties: map[string]any{"down": true}},
		"flowing":    {Name: "minecraft:water", Properties: map[string]any{"level": int32(1)}},
	} {
		state, err := world.ResolveState(world.DefaultBlockRegistry, source)
		if err != nil {
			t.Fatal(err)
		}
		states[name] = state
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s/xor=%v", tc.Name, tc.Xoroshiro), func(t *testing.T) {
			region := countLayerTestRegion()
			source := sculkFeatureSource{terrain: tc.Terrain, states: states}
			region.f.source = source
			for x := uint8(0); x < 16; x++ {
				for z := uint8(0); z < 16; z++ {
					for y := int16(0); y < 32; y++ {
						primary, liquid := source.BaseState(int32(x), int32(y), int32(z))
						region.c.SetBlock(x, y, z, 0, primary)
						region.c.SetBlock(x, y, z, 1, liquid)
					}
				}
			}
			config := map[string]any{"charge_count": float64(tc.Charges), "amount_per_charge": float64(tc.Amount), "spread_attempts": float64(tc.Attempts), "growth_rounds": float64(tc.GrowthRounds), "spread_rounds": float64(tc.SpreadRounds)}
			random := java26.NewLegacyRandom(tc.Seed)
			if tc.Xoroshiro {
				random = java26.NewXoroshiroRandom(tc.Seed)
			}
			if got := region.f.placeConfiguredData(region, "sculk_patch", config, featurePos{8, 8, 8}, &random); got != tc.Placed {
				t.Errorf("placed=%v source=%v", got, tc.Placed)
			}
			if next := random.NextLong(); next != tc.NextLong {
				t.Errorf("following stream=%d, source=%d", next, tc.NextLong)
			}
			if len(region.overlay) != len(tc.Writes) {
				t.Errorf("written positions=%d, source=%d", len(region.overlay), len(tc.Writes))
			}
			mismatches := 0
			for _, input := range tc.Writes {
				p := featurePos{input.Position[0], input.Position[1], input.Position[2]}
				block, ok := featureStateBlock(input.State)
				if !ok {
					t.Fatal("invalid source block")
				}
				want, err := block.ResolveNative(region.c.BlockRegistry())
				if err != nil {
					t.Fatal(err)
				}
				if got, ok := region.overlay[p]; !ok || got != want {
					mismatches++
					if mismatches <= 5 {
						t.Errorf("at %v: got=%+v present=%v source projection=%+v", p, got, ok, want)
					}
				}
			}
			if mismatches > 5 {
				t.Errorf("total block mismatches=%d", mismatches)
			}
		})
	}
}
