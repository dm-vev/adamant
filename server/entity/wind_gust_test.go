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
