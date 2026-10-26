package world_test

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/biome"
)

type lightningSearchBiome struct {
	biome.Plains
	id            int
	temperature   float64
	precipitation bool
}

func (b lightningSearchBiome) EncodeBiome() int       { return b.id }
func (b lightningSearchBiome) Temperature() float64   { return b.temperature }
func (b lightningSearchBiome) HasPrecipitation() bool { return b.precipitation }
func (b lightningSearchBiome) Rainfall() float64 {
	if b.precipitation {
		return .8
	}
	return 0
}
func (b lightningSearchBiome) String() string { return fmt.Sprintf("audit_lightning_search_%d", b.id) }

var lightningSearchBiomesOnce sync.Once
var lightningSearchBiomes [3][2]world.Biome

func TestLightningSearchMatchesExecutedClient(t *testing.T) {
	data, err := os.ReadFile("../block/testdata/bedrock_1_26_51_lightning_search.json")
	if err != nil {
		t.Fatal(err)
	}
	var reference struct {
		Complete bool
		Cases    []struct {
			Name       string
			Center     cube.Pos
			Raining    *bool
			Candidates []struct {
				Pos                   cube.Pos
				Material              int
				Covered               bool
				Precipitation, Loaded *bool
				Temperature           *float64
			}
			Selected []int
		}
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if !reference.Complete || len(reference.Cases) != 670 {
		t.Fatal("incomplete native selection capture")
	}
	lightningSearchBiomesOnce.Do(func() {
		for i, temp := range []float64{.15, .15000002, .8} {
			for p := range 2 {
				// The native probe supplies effective temperature at Y=101. This
				// adapter reproduces that input after the world's altitude drop;
				// it does not validate the native temperature getter itself.
				b := lightningSearchBiome{id: 49000 + i*2 + p, temperature: temp + 37.0/600, precipitation: p == 1}
				world.RegisterBiome(b)
				lightningSearchBiomes[i][p] = b
			}
		}
	})
	materials := []string{"lightning_rod", "exposed_lightning_rod", "weathered_lightning_rod", "oxidized_lightning_rod",
		"waxed_lightning_rod", "waxed_exposed_lightning_rod", "waxed_weathered_lightning_rod", "waxed_oxidized_lightning_rod", "stone"}
	for i, trace := range reference.Cases {
		t.Run(fmt.Sprintf("%03d/%s", i, trace.Name), func(t *testing.T) {
			w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
			defer w.Close()
			w.StopWeatherCycle()
			if trace.Raining == nil || *trace.Raining {
				w.StartRaining(time.Hour)
			} else {
				w.StopRaining()
			}
			<-w.Exec(func(tx *world.Tx) {
				for _, c := range trace.Candidates {
					if c.Loaded != nil && !*c.Loaded {
						continue
					}
					var b world.Block = block.Stone{}
					if c.Material < 8 {
						var ok bool
						b, ok = world.BlockByName("minecraft:"+materials[c.Material], map[string]any{"facing_direction": int32(1), "powered_bit": false})
						if !ok {
							t.Fatal("missing rod", materials[c.Material])
						}
					}
					tx.SetBlock(c.Pos, b, nil)
					if c.Covered {
						tx.SetBlock(c.Pos.Add(cube.Pos{0, 2, 0}), block.Stone{}, nil)
					}
					if c.Temperature != nil {
						index := 2
						if *c.Temperature == .15 {
							index = 0
						} else if *c.Temperature == .15000002 {
							index = 1
						}
						p := 1
						if c.Precipitation != nil && !*c.Precipitation {
							p = 0
						}
						tx.SetBiome(c.Pos.Side(cube.FaceUp), lightningSearchBiomes[index][p])
					}
				}
				before := map[world.ChunkPos]bool{}
				for x := -5; x <= 5; x++ {
					for z := -5; z <= 5; z++ {
						p := world.ChunkPos{int32(x), int32(z)}
						before[p], _ = tx.ChunkState(p)
					}
				}
				got, found := tx.FindLightningRod(trace.Center)
				if found != (len(trace.Selected) == 3) || found && got != (cube.Pos{trace.Selected[0], trace.Selected[1], trace.Selected[2]}) {
					t.Errorf("selected %v,%v; native=%v", got, found, trace.Selected)
				}
				for p, loaded := range before {
					if after, _ := tx.ChunkState(p); after != loaded {
						t.Errorf("query loaded chunk %v", p)
					}
				}
			})
		})
	}
}
