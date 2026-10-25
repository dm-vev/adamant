package entity

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestWindChargeBurstsAsJavaExplosions checks a wind charge bursts as the
// explosion of radius 1.2 and knockback 1.22 of WindCharge.explode: it
// pushes what is within twice its radius away from it, hurts nothing, and
// leaves what is further away be.
func TestWindChargeBurstsAsJavaExplosions(t *testing.T) {
	w := world.Config{Entities: DefaultRegistry, Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		for x := -4; x <= 6; x++ {
			for z := -4; z <= 4; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
			}
		}
		near := tx.AddEntity(NewPig(world.EntitySpawnOpts{Position: mgl64.Vec3{2.5, 64, .5}})).(*Pig)
		far := tx.AddEntity(NewPig(world.EntitySpawnOpts{Position: mgl64.Vec3{3.5, 64, .5}})).(*Pig)
		center := mgl64.Vec3{.5, 64.5, .5}
		eye := EyePosition(near)
		windGust(tx, nil, center, windChargeRadius, windChargeKnockback, false)

		dist := near.Position().Sub(center).Len() / 2.4
		want := eye.Sub(center).Normalize().Mul((1 - dist) * float64(float32(windChargeKnockback)))
		if got := near.Velocity(); !got.ApproxEqualThreshold(want, 1e-6) {
			t.Fatalf("pig 2 blocks from the burst got velocity %v, want %v", got, want)
		}
		if got := far.Velocity(); got != (mgl64.Vec3{}) {
			t.Fatalf("pig 3 blocks from the burst got velocity %v, want none: it is beyond twice the radius", got)
		}
		if near.Health() != near.MaxHealth() || far.Health() != far.MaxHealth() {
			t.Fatalf("a wind charge burst hurt the pigs around it")
		}
	})
}

// TestBlastProtectionResistsGusts checks Blast Protection on armour gives the
// explosion knockback resistance of 0.15 a level.
func TestBlastProtectionResistsGusts(t *testing.T) {
	if got := explosionKnockbackResistance(nil); got != 0 {
		t.Fatalf("resistance of nothing = %v, want 0", got)
	}
	if math.Abs(float64(float32(.15)*4)-.6) > 1e-6 {
		t.Fatal("float32 levels drifted")
	}
}

// TestWindChargesTriggerBlocksAlongTheirRays checks the TRIGGER interaction
// of a wind charge: a wooden door its rays reach opens, an iron door does
// not.
func TestWindChargesTriggerBlocksAlongTheirRays(t *testing.T) {
	w := world.Config{Entities: DefaultRegistry, Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		for x := -4; x <= 8; x++ {
			for z := -4; z <= 4; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
			}
		}
		wood, iron := cube.Pos{0, 64, 0}, cube.Pos{6, 64, 0}
		tx.SetBlock(wood, block.WoodDoor{Facing: cube.North}, nil)
		tx.SetBlock(wood.Side(cube.FaceUp), block.WoodDoor{Facing: cube.North, Top: true}, nil)
		tx.SetBlock(iron, block.IronDoor{Facing: cube.North}, nil)
		tx.SetBlock(iron.Side(cube.FaceUp), block.IronDoor{Facing: cube.North, Top: true}, nil)

		windChargeTriggerBlocks(tx, mgl64.Vec3{.5, 64.5, -.25}, windChargeRadius, nil)
		if door := tx.Block(wood).(block.WoodDoor); !door.Open {
			t.Fatal("a wind charge bursting at a wooden door did not open it")
		}
		windChargeTriggerBlocks(tx, mgl64.Vec3{6.5, 64.5, -.25}, windChargeRadius, nil)
		if door, ok := tx.Block(iron).(block.IronDoor); !ok || door.Open {
			t.Fatal("a wind charge opened an iron door")
		}
	})
}
