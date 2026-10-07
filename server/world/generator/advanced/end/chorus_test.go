package end

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

func TestChorusPlantMatchesJava263(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_chorus.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Terrain        string
		Y              int
		Seed, NextLong int64
		Xoroshiro      bool
		Writes         []struct {
			Position [3]int
			State    json.RawMessage
		}
	}
	if err = json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	world.DefaultBlockRegistry.Finalize()
	g := New(0)
	stone, err := world.ResolveState(world.DefaultBlockRegistry, world.BlockState{Name: "minecraft:stone"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s/y=%d/seed=%d/xor=%v", tc.Terrain, tc.Y, tc.Seed, tc.Xoroshiro), func(t *testing.T) {
			region := &endFeatureRegion{minChunkX: -2, minChunkZ: -2, minY: 0, maxY: 255, airRID: g.airRID}
			for cx := -2; cx <= 2; cx++ {
				for cz := -2; cz <= 2; cz++ {
					c := chunk.New(world.DefaultBlockRegistry, cube.Range{0, 255})
					region.chunks[region.index(cx, cz)] = c
					for x := uint8(0); x < 16; x++ {
						for z := uint8(0); z < 16; z++ {
							c.SetBlock(x, int16(tc.Y-1), z, 0, g.endStoneRID)
							if tc.Terrain == "ceiling" && tc.Y+3 <= 255 {
								c.SetBlock(x, int16(tc.Y+3), z, 0, stone.Block)
							}
							if tc.Terrain == "wall" && cx*16+int(x) == 10 {
								for y := tc.Y; y <= 255; y++ {
									c.SetBlock(x, int16(y), z, 0, stone.Block)
								}
							}
						}
					}
				}
			}
			random := newEndRand(tc.Seed)
			if tc.Xoroshiro {
				random = newEndRandWithRandom(java26.NewXoroshiroRandom(tc.Seed))
			}
			g.generateChorusPlant(endFeatureOrigin{region: region}, 8, tc.Y, 8, random, 8)
			if next := random.Long(); next != tc.NextLong {
				t.Errorf("following RNG=%d source=%d", next, tc.NextLong)
			}
			written := 0
			for _, overlay := range region.overlays {
				written += len(overlay) / 2
			}
			if written != len(tc.Writes) {
				t.Errorf("written positions=%d source=%d", written, len(tc.Writes))
			}
			mismatches := 0
			for _, b := range tc.Writes {
				var state world.BlockState
				if b.State[0] == '"' {
					if err := json.Unmarshal(b.State, &state.Name); err != nil {
						t.Fatal(err)
					}
				} else {
					var raw struct {
						ID         string
						Properties map[string]any
					}
					if err := json.Unmarshal(b.State, &raw); err != nil {
						t.Fatal(err)
					}
					state = world.BlockState{Name: raw.ID, Properties: raw.Properties}
				}
				want, err := world.ResolveState(world.DefaultBlockRegistry, state)
				if err != nil {
					t.Fatal(err)
				}
				if got := region.block(b.Position[0], b.Position[1], b.Position[2]); got != want.Block {
					mismatches++
					if mismatches <= 5 {
						t.Errorf("at %v got=%d source=%d", b.Position, got, want.Block)
					}
				}
			}
			if mismatches > 5 {
				t.Errorf("total mismatches=%d", mismatches)
			}
		})
	}
}
