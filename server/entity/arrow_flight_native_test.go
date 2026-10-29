package entity

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
)

func TestArrowFreeFlightMatchesBDS(t *testing.T) {
	data, err := os.ReadFile("../block/testdata/bedrock_1_26_51_arrow_flight.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Height, Phase int
				Velocity      [3]float64
			}
			SetupTick int64 `json:"setup_tick"`
			Spawn     struct {
				ID                 string
				Position, Velocity pistonEntityNativeVec
			}
			Changes []struct{ Tick int64 }
			Samples []struct {
				Tick       int64
				Projectile *pistonEntityNativeBody
			}
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 28 {
		t.Fatal("incomplete native arrow flight corpus")
	}
	for _, trace := range native.Scenarios {
		c := trace.Scenario
		t.Run(fmt.Sprintf("height%d/phase%d/velocity%v", c.Height, c.Phase, c.Velocity), func(t *testing.T) {
			if len(trace.Samples) != 64 || len(trace.Changes) != 1 || trace.Spawn.ID != "minecraft:arrow" || trace.Changes[0].Tick%2 != int64(c.Phase) {
				t.Fatal("invalid native arrow flight controls")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick, settings.WeatherCycle = trace.SetupTick-1, false
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry, Provider: world.NopProvider{Set: settings}}.New()
			loader := world.NewLoader(4, w, world.NopViewer{})
			defer func() { <-w.Exec(func(tx *world.Tx) { loader.Close(tx) }); _ = w.Close() }()
			_ = w.SetGameRule("spawn_mobs", false)
			<-w.Exec(func(tx *world.Tx) { loader.Move(tx, trace.Spawn.Position.vec()); loader.Load(tx, 81) })
			var arrow *world.EntityHandle
			for _, f := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != f.Tick {
						t.Fatal("native arrow flight clock differs")
					}
					if f.Projectile != nil {
						if arrow == nil {
							t.Fatal("native flight arrow missing")
						}
						e, ok := arrow.Entity(tx)
						if !ok {
							t.Fatal("flight arrow disappeared")
						}
						comparePistonVanillaBody(t, f.Tick, e.(*Ent), *f.Projectile)
						if e.Position() != f.Projectile.Position.vec() || e.(*Ent).Velocity() != f.Projectile.Velocity.vec() {
							t.Errorf("tick %d arrow binary32 state differs from native flight", f.Tick)
						}
					} else if arrow != nil {
						t.Fatal("kernel arrow outlived native reference")
					}
					if f.Tick == trace.Changes[0].Tick {
						conf := arrowConf
						conf.DisablePickup = true
						arrow = (world.EntitySpawnOpts{Position: trace.Spawn.Position.vec(), Velocity: trace.Spawn.Velocity.vec()}).New(ArrowType, conf)
						tx.AddEntity(arrow)
					}
				})
				if t.Failed() {
					break
				}
			}
		})
	}
}
