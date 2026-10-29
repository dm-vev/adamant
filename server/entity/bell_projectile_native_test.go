package entity

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func TestBellArrowCollisionsMatchBDS(t *testing.T) {
	data, err := os.ReadFile("../block/testdata/bedrock_1_26_51_bell_projectiles.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Readback  struct{ Snapshots int } `json:"actor_readback"`
		Scenarios []struct {
			Scenario struct {
				Attachment                 string
				Direction, Approach, Phase int
			}
			Base      cube.Pos
			SetupTick int64 `json:"setup_tick"`
			Spawn     struct {
				ID                 string
				Position, Velocity pistonEntityNativeVec
			}
			Changes []struct {
				Tick int64
				Kind string
			}
			Hits []struct {
				Tick     int64
				Position pistonEntityNativeVec
				Face     string
			}
			Samples []struct {
				Tick       int64
				Block      pistonEntityNativeBlock
				Actor      nativeBellSavedActor
				Projectile *pistonEntityNativeBody
			}
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 64 || native.Readback.Snapshots != 5120 {
		t.Fatal("incomplete native bell arrow corpus")
	}
	for _, trace := range native.Scenarios {
		c := trace.Scenario
		t.Run(fmt.Sprintf("%s/direction%d/approach%d", c.Attachment, c.Direction, c.Approach), func(t *testing.T) {
			if len(trace.Samples) != 80 || len(trace.Changes) != 1 || len(trace.Hits) != 1 || trace.Spawn.ID != "minecraft:arrow" || trace.Changes[0].Tick%2 != int64(c.Phase) || cube.PosFromVec3(trace.Hits[0].Position.vec()) != trace.Base {
				t.Fatal("invalid native bell arrow controls")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick, settings.WeatherCycle = trace.SetupTick-1, false
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry, Provider: world.NopProvider{Set: settings}}.New()
			loader := world.NewLoader(2, w, world.NopViewer{})
			defer func() { <-w.Exec(func(tx *world.Tx) { loader.Close(tx) }); _ = w.Close() }()
			_ = w.SetGameRule("spawn_mobs", false)
			<-w.Exec(func(tx *world.Tx) {
				loader.Move(tx, trace.Base.Vec3Centre())
				loader.Load(tx, 25)
				tx.SetBlock(trace.Base.Side(cube.FaceDown), block.Stone{}, nil)
				b, ok := tx.World().BlockRegistry().BlockByName("minecraft:bell", map[string]any{"attachment": c.Attachment, "direction": int32(c.Direction), "toggle_bit": uint8(0)})
				if !ok {
					t.Fatal("native bell arrow permutation missing")
				}
				bell := b.(block.Bell)
				if c.Attachment == "hanging" {
					tx.SetBlock(trace.Base.Side(cube.FaceUp), block.Stone{}, nil)
				}
				if c.Attachment == "side" || c.Attachment == "multiple" {
					tx.SetBlock(trace.Base.Side(bell.Facing.Face().Opposite()), block.Stone{}, nil)
					if c.Attachment == "multiple" {
						tx.SetBlock(trace.Base.Side(bell.Facing.Face()), block.Stone{}, nil)
					}
				}
				tx.SetBlock(trace.Base, bell, nil)
			})
			var arrow *world.EntityHandle
			for _, f := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != f.Tick {
						t.Fatal("native bell arrow clock differs")
					}
					comparePistonEntityNativeBlock(t, tx, f.Tick, trace.Base, f.Block)
					compareNativeBellSavedActor(t, tx, f.Tick, trace.Base, f.Actor)
					if f.Projectile != nil {
						if arrow == nil {
							t.Fatal("native arrow missing")
						}
						e, ok := arrow.Entity(tx)
						if !ok {
							t.Fatal("arrow disappeared before native reference")
						}
						comparePistonVanillaBody(t, f.Tick, e.(*Ent), *f.Projectile)
					} else if arrow != nil {
						t.Fatal("kernel arrow outlived native arrow")
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
