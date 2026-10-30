package entity

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestArrowButtonSignalsMatchNativeCapture(t *testing.T) {
	data, err := os.ReadFile("../block/testdata/bedrock_1_26_51_button_projectiles.json")
	if err != nil {
		t.Fatal(err)
	}
	type vector struct{ X, Y, Z float64 }
	vec := func(v vector) mgl64.Vec3 { return mgl64.Vec3{v.X, v.Y, v.Z} }
	var reference struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Block         string
				Facing, Phase int
			}
			X, Z    int
			Spawn   struct{ Position, Velocity vector }
			Samples []struct {
				Tick       int64
				Projectile *struct{ Position vector }
				Source     struct {
					States struct {
						Pressed bool `json:"button_pressed_bit"`
					}
				}
				Wire struct {
					States struct {
						Signal int `json:"redstone_signal"`
					}
				}
			}
			Changes []struct {
				Tick int64
				Kind string
			}
		}
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if !reference.Complete || len(reference.Scenarios) != 180 {
		t.Fatal("incomplete native arrow/button capture")
	}
	for _, trace := range reference.Scenarios {
		c := trace.Scenario
		t.Run(fmt.Sprintf("%s/facing%d/phase%d", c.Block, c.Facing, c.Phase), func(t *testing.T) {
			if len(trace.Samples) != 100 || len(trace.Changes) != 2 {
				t.Fatal("invalid native arrow/button trace")
			}
			var button block.Button
			found := false
			for _, k := range block.ButtonTypes() {
				b := block.Button{Type: k, Facing: cube.Face(c.Facing)}
				id, _ := b.EncodeItem()
				if id == "minecraft:"+c.Block {
					button = b
					found = true
					break
				}
			}
			if !found {
				t.Fatal("unknown native button")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = trace.Samples[0].Tick - 2
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Dim: world.End, Provider: world.NopProvider{Set: settings}}.New()
			t.Cleanup(func() { _ = w.Close() })
			pos, wirePos := cube.Pos{0, 65, 0}, cube.Pos{1, 65, 0}
			if c.Facing == 4 {
				wirePos[0] = -1
			}
			<-w.Exec(func(tx *world.Tx) {
				tx.SetBlock(pos.Side(button.Facing.Opposite()), block.Stone{}, nil)
				tx.SetBlock(wirePos.Side(cube.FaceDown), block.Stone{}, nil)
				tx.SetBlock(pos, button, nil)
				tx.SetBlock(wirePos, block.RedstoneWire{}, nil)
			})
			var arrow *world.EntityHandle
			for _, sample := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					b, ok := tx.Block(pos).(block.Button)
					if !ok {
						t.Error("button disappeared")
						return
					}
					if b.Pressed != sample.Source.States.Pressed {
						t.Errorf("tick %d pressed=%v native=%v", sample.Tick, b.Pressed, sample.Source.States.Pressed)
					}
					wire := tx.Block(wirePos).(block.RedstoneWire)
					if wire.Power != sample.Wire.States.Signal {
						t.Errorf("tick %d wire=%d native=%d", sample.Tick, wire.Power, sample.Wire.States.Signal)
					}
					if arrow != nil && sample.Projectile != nil && sample.Tick >= trace.Changes[0].Tick+4 {
						e, exists := arrow.Entity(tx)
						if !exists {
							t.Error("arrow disappeared before native removal")
							return
						}
						behaviour := e.(*Ent).data.Data.(*ProjectileBehaviour)
						if !behaviour.collided || behaviour.collisionPos != pos {
							t.Errorf("tick %d arrow did not remain attached to the native button face", sample.Tick)
						}
						axis := 0
						if c.Facing < 2 {
							axis = 1
						} else if c.Facing < 4 {
							axis = 2
						}
						native := vec(sample.Projectile.Position).Sub(mgl64.Vec3{float64(trace.X), 0, float64(trace.Z)})
						if math.Abs(e.Position()[axis]-native[axis]) > 0.000001 {
							t.Errorf("tick %d impact axis=%g native=%g", sample.Tick, e.Position()[axis], native[axis])
						}
					}
					if sample.Tick == trace.Changes[0].Tick {
						conf := arrowConf
						conf.DisablePickup = true
						location := vec(trace.Spawn.Position).Sub(mgl64.Vec3{float64(trace.X), 0, float64(trace.Z)})
						arrow = (world.EntitySpawnOpts{Position: location, Velocity: vec(trace.Spawn.Velocity)}).New(ArrowType, conf)
						tx.AddEntity(arrow)
					}
					if sample.Tick == trace.Changes[1].Tick {
						if arrow == nil {
							t.Error("arrow missing at removal")
							return
						}
						e, ok := arrow.Entity(tx)
						if !ok {
							t.Error("arrow disappeared before removal")
							return
						}
						tx.RemoveEntity(e)
						_ = arrow.Close()
						arrow = nil
					}
				})
				if t.Failed() {
					break
				}
			}
		})
	}
}
