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

func TestBellWindChargeCollisionsMatchBDS(t *testing.T) {
	testBellWindChargeCollisionsNative(t, false)
}

func TestBellBreezeWindChargeCollisionsMatchBDS(t *testing.T) {
	testBellWindChargeCollisionsNative(t, true)
}

func testBellWindChargeCollisionsNative(t *testing.T, breeze bool) {
	t.Helper()
	file, projectileID := "bedrock_1_26_51_bell_wind_projectiles.json", "minecraft:wind_charge_projectile"
	if breeze {
		file, projectileID = "bedrock_1_26_51_bell_breeze_projectiles.json", "minecraft:breeze_wind_charge_projectile"
	}
	data, err := os.ReadFile("../block/testdata/" + file)
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Readback  struct{ Snapshots int } `json:"actor_readback"`
		Scenarios []struct {
			Scenario struct {
				Attachment, Projectile     string
				Direction, Approach, Phase int
				Repeat                     bool
			}
			Base      cube.Pos
			SetupTick int64 `json:"setup_tick"`
			Changes   []struct {
				Tick  int64
				Kind  string
				Spawn struct {
					ID                 string
					Position, Velocity pistonEntityNativeVec
					NativeBirth        *struct {
						pistonEntityNativeBody
						Tick      int64
						OwnerID   string `json:"owner_id"`
						OwnerType string `json:"owner_type"`
					} `json:"native_birth"`
				}
			}
			Hits []struct {
				Tick     int64
				Position pistonEntityNativeVec
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
		t.Fatal("incomplete native bell wind-charge corpus")
	}
	for _, trace := range native.Scenarios {
		c := trace.Scenario
		t.Run(fmt.Sprintf("%s/direction%d/approach%d/repeat%t/phase%d", c.Attachment, c.Direction, c.Approach, c.Repeat, c.Phase), func(t *testing.T) {
			shots := 1
			if c.Repeat {
				shots++
			}
			if len(trace.Samples) != 80 || len(trace.Changes) != shots || len(trace.Hits) != shots || c.Projectile != projectileID {
				t.Fatal("invalid native bell wind-charge controls")
			}
			for i, change := range trace.Changes {
				hitPos := cube.PosFromVec3(trace.Hits[i].Position.vec())
				wantHit := trace.Base
				// Teleporting a real breeze charge leaves it inside the front
				// support for this approach. Native still activates the adjacent
				// bell through the burst; the original hit cell is retained.
				if breeze && c.Attachment == "multiple" && c.Approach == c.Direction {
					wantHit = trace.Base.Side([4]cube.Face{cube.FaceSouth, cube.FaceWest, cube.FaceNorth, cube.FaceEast}[c.Direction])
				}
				if change.Spawn.ID != c.Projectile || change.Kind != "spawn" || change.Tick != trace.SetupTick+40+int64(c.Phase+i*10) || hitPos != wantHit {
					t.Fatal("native shot clock, identity or bell collision differs")
				}
				if breeze {
					birth := change.Spawn.NativeBirth
					if birth == nil || birth.ID != projectileID || birth.Tick >= change.Tick || birth.OwnerID == "" || birth.OwnerType != "minecraft:breeze" {
						t.Fatal("native charge was not captured from a real breeze owner")
					}
				}
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
					t.Fatal("native bell wind-charge permutation missing")
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
			var projectile *world.EntityHandle
			for _, f := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != f.Tick {
						t.Fatal("native bell wind-charge clock differs")
					}
					comparePistonEntityNativeBlock(t, tx, f.Tick, trace.Base, f.Block)
					compareNativeBellSavedActor(t, tx, f.Tick, trace.Base, f.Actor)
					var e world.Entity
					if projectile != nil {
						e, _ = projectile.Entity(tx)
					}
					if f.Projectile != nil {
						if e == nil {
							t.Fatal("kernel wind charge disappeared before native reference")
						}
						comparePistonVanillaBody(t, f.Tick, e.(*Ent), *f.Projectile)
					} else if e != nil {
						t.Fatal("kernel wind charge outlived native reference")
					}
					for _, change := range trace.Changes {
						if change.Tick == f.Tick {
							opts := world.EntitySpawnOpts{Position: change.Spawn.Position.vec(), Velocity: change.Spawn.Velocity.vec()}
							if breeze {
								projectile = NewBreezeWindCharge(opts, nil)
							} else {
								projectile = NewWindCharge(opts, nil)
							}
							launched := tx.AddEntity(projectile)
							if breeze {
								// The control inputs teleport/shoot an existing native
								// actor: translate its measured birth AABB accordingly.
								birth := change.Spawn.NativeBirth.pistonEntityNativeBody
								birth.AABB.Center.X += opts.Position[0] - birth.Position.X
								birth.AABB.Center.Y += opts.Position[1] - birth.Position.Y
								birth.AABB.Center.Z += opts.Position[2] - birth.Position.Z
								birth.Position, birth.Velocity = change.Spawn.Position, change.Spawn.Velocity
								comparePistonVanillaBody(t, f.Tick, launched.(*Ent), birth)
							}
						}
					}
				})
				if t.Failed() {
					break
				}
			}
		})
	}
}
