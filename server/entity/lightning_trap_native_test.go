package entity_test

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestSkeletonTrapLightningSelectionMatchesBDS(t *testing.T) {
	data, err := os.ReadFile("../block/testdata/bedrock_1_26_51_lightning_traps.json")
	if err != nil {
		t.Fatal(err)
	}
	type vector struct{ X, Y, Z float64 }
	vec := func(v vector) mgl64.Vec3 { return mgl64.Vec3{v.X, v.Y, v.Z} }
	var native struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Name, Weather string
				Rods          []struct{ X int }
			}
			Changes []struct{ Horse vector }
			Bolts   []struct{ Location vector }
			Blocks  []struct {
				Position cube.Pos
				ID       string
				States   map[string]any
			}
			Samples []struct {
				Rods []struct {
					States struct {
						Powered bool `json:"powered_bit"`
					}
				}
			}
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 26 {
		t.Fatal("incomplete original-BDS trap capture")
	}
	world.DefaultBlockRegistry.Finalize()
	byName := map[string][]world.Block{}
	for _, b := range world.Blocks() {
		name, _ := b.EncodeBlock()
		byName[name] = append(byName[name], b)
	}
	for _, trace := range native.Scenarios {
		t.Run(trace.Scenario.Name, func(t *testing.T) {
			if len(trace.Bolts) != 1 || len(trace.Changes) != 1 {
				t.Fatal("missing native trap lightning positive control")
			}
			w := newDeathTestWorld(t)
			w.StopWeatherCycle()
			if trace.Scenario.Weather == "clear" {
				w.StopRaining()
			} else {
				w.StartRaining(time.Hour)
			}
			<-w.Exec(func(tx *world.Tx) {
				for x := -4; x <= 70; x++ {
					for z := -4; z <= 4; z++ {
						tx.SetBlock(cube.Pos{x, 64, z}, block.Stone{}, nil)
					}
				}
				for _, v := range trace.Blocks {
					for name, value := range v.States {
						if number, ok := value.(float64); ok {
							v.States[name] = int32(number)
						}
					}
					b, ok := world.BlockByName(v.ID, v.States)
					if !ok {
						// BDS retains legacy aliases such as stone_type/color/wood_type
						// alongside split identifiers. Match every canonical state;
						// only redundant native aliases are allowed to remain extra.
						for _, candidate := range byName[v.ID] {
							_, canonical := candidate.EncodeBlock()
							match := true
							for key, value := range canonical {
								if v.States[key] != value {
									match = false
									break
								}
							}
							if match {
								b, ok = candidate, true
								break
							}
						}
					}
					if !ok {
						t.Fatalf("missing native block %s %v", v.ID, v.States)
					}
					tx.SetBlock(v.Position, b, nil)
				}
			})
			for range 24 {
				w.AdvanceTick()
			}
			<-w.Exec(func(tx *world.Tx) {
				tx.AddEntity(entity.NewSkeletonHorseTrap(world.EntitySpawnOpts{Position: vec(trace.Changes[0].Horse)}))
				addSurvivalPlayer(tx, mgl64.Vec3{.5, 65, 4.5})
			})
			for range 3 {
				w.AdvanceTick()
			}
			<-w.Exec(func(tx *world.Tx) {
				bolts := 0
				for e := range tx.Entities() {
					if e.H().Type() == entity.LightningType {
						bolts++
						if got, want := e.Position(), vec(trace.Bolts[0].Location); got != want {
							t.Errorf("trap bolt %v, BDS %v", got, want)
						}
					}
				}
				if bolts != 1 {
					t.Errorf("trap spawned %d bolts, BDS one", bolts)
				}
				for i, r := range trace.Scenario.Rods {
					want := false
					for _, s := range trace.Samples {
						want = want || s.Rods[i].States.Powered
					}
					if got := tx.Block(cube.Pos{r.X, 65, 0}).(block.LightningRod).Powered; got != want {
						t.Errorf("rod X=%d powered=%v, BDS=%v", r.X, got, want)
					}
				}
			})
		})
	}
}
