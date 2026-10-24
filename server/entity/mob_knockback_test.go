package entity_test

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestKnockBackKeepsHalfTheSpeed checks mobs are knocked back as
// LivingEntity.knockback does: half their speed is kept, the push is added,
// only a mob on the ground is lifted, and resistance weakens the push.
func TestKnockBackKeepsHalfTheSpeed(t *testing.T) {
	w := world.Config{Entities: entity.DefaultRegistry, Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		near := func(got, want mgl64.Vec3) bool {
			return got.Sub(want).Len() < 1e-9
		}
		flying := tx.AddEntity(entity.NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 100, .5}})).(*entity.Cow)
		flying.SetVelocity(mgl64.Vec3{1, -.5, 0})
		flying.KnockBack(mgl64.Vec3{-1.5, 100, .5}, .4, .4)
		if got := flying.Velocity(); !near(got, mgl64.Vec3{.9, -.5, 0}) {
			t.Fatalf("cow in the air knocked to %v, want half its speed plus the push and no lift", got)
		}

		for x := -2; x <= 2; x++ {
			for z := -2; z <= 2; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
			}
		}
		grounded := tx.AddEntity(entity.NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 64, .5}})).(*entity.Cow)
		for tick := int64(1); !grounded.OnGround(); tick++ {
			if tick > 20 {
				t.Fatal("cow did not land")
			}
			grounded.Tick(tx, tick)
		}
		grounded.SetVelocity(mgl64.Vec3{0, -.08, 0})
		grounded.KnockBack(mgl64.Vec3{.5, 64, -1.5}, .4, .4)
		if got := grounded.Velocity(); !near(got, mgl64.Vec3{0, math.Min(.4, -.04+.4), .4}) {
			t.Fatalf("cow on the ground knocked to %v, want lifted by min(0.4, vy/2+0.4)", got)
		}

		hoglin := tx.AddEntity(entity.NewHoglin(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 100, 10.5}})).(*entity.Hoglin)
		hoglin.SetVelocity(mgl64.Vec3{})
		hoglin.KnockBack(mgl64.Vec3{-1.5, 100, 10.5}, 1, .4)
		if got := hoglin.Velocity()[0]; math.Abs(got-.4) > 1e-9 {
			t.Fatalf("hoglin knocked to %v, want 1 less its resistance of 0.6", got)
		}
	})
}
