package entity

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestHoneyEntitiesMatchBDS(t *testing.T) {
	data, err := os.ReadFile("../block/testdata/bedrock_1_26_51_honey_entities.json")
	if err != nil {
		t.Fatal(err)
	}
	type point struct{ X, Y, Z float64 }
	vec := func(p point) mgl64.Vec3 { return mgl64.Vec3{p.X, p.Y, p.Z} }
	var native struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Entity      string
				Axis        int
				Gap, Height float64
				VelocityY   float64 `json:"velocity_y"`
			}
			Base    cube.Pos
			Spawn   struct{ Position, Velocity point }
			Samples []struct {
				Position, Velocity point
				OnGround           bool `json:"on_ground"`
			}
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 124 {
		t.Fatal("incomplete original-BDS honey entities fixture")
	}
	for _, c := range native.Scenarios {
		s := c.Scenario
		t.Run(fmt.Sprintf("%s/axis%d/gap%g/height%g/velocity%g", s.Entity, s.Axis, s.Gap, s.Height, s.VelocityY), func(t *testing.T) {
			if len(c.Samples) != 20 {
				t.Fatal("missing native honey frames")
			}
			w := world.Config{Synchronous: true, Entities: DefaultRegistry}.New()
			t.Cleanup(func() { _ = w.Close() })
			if err := w.Do(func(tx *world.Tx) {
				for x := -4; x <= 4; x++ {
					for z := -4; z <= 4; z++ {
						tx.SetBlock(cube.Pos{c.Base[0] + x, 64, c.Base[2] + z}, block.Stone{}, nil)
					}
				}
				for y := 65; y <= 67; y++ {
					tx.SetBlock(cube.Pos{c.Base[0], y, c.Base[2]}, block.HoneyBlock{}, nil)
				}
				velocity := mgl64.Vec3{0, s.VelocityY, .16}
				if s.Axis == 2 {
					velocity = mgl64.Vec3{.16, s.VelocityY, 0}
				}
				opts := world.EntitySpawnOpts{Position: vec(c.Spawn.Position), Velocity: velocity}
				var handle *world.EntityHandle
				if s.Entity == "minecraft:item" {
					handle = NewItem(opts, item.NewStack(item.Diamond{}, 1))
				} else {
					handle = NewArmourStand(opts)
				}
				e := tx.AddEntity(handle).(*Ent)
				for n, frame := range c.Samples {
					before := e.Position()
					e.Tick(tx, int64(n))
					motion := e.Velocity()
					if s.Entity == "minecraft:armor_stand" {
						motion = e.Position().Sub(before)
					}
					if !e.Position().ApproxEqualThreshold(vec(frame.Position), 2e-5) || !motion.ApproxEqualThreshold(vec(frame.Velocity), 2e-5) || e.OnGround() != frame.OnGround {
						t.Errorf("frame %d: pos=%v motion=%v ground=%t, original=%v %v %t", n+1, e.Position(), motion, e.OnGround(), vec(frame.Position), vec(frame.Velocity), frame.OnGround)
						return
					}
				}
			}).Wait(context.Background()); err != nil {
				t.Fatal(err)
			}
		})
	}
}
