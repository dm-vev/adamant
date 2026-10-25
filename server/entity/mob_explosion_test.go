package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestExplosionHurtsMobs checks explosions hurt and push living mobs, as
// ServerExplosion.hurtEntities does.
func TestExplosionHurtsMobs(t *testing.T) {
	w := dragonTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		cow := tx.AddEntity(entity.NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{4.5, 64, .5}})).(entity.Living)
		before := cow.Health()
		block.ExplosionConfig{NoBlockDamage: true}.Explode(tx, dragonTestExplosion{mgl64.Vec3{-.5, 64.5, .5}, 3})
		if cow.Health() >= before {
			t.Fatalf("cow health %v after an explosion five blocks away, was %v", cow.Health(), before)
		}
		if cow.Velocity()[0] <= 0 {
			t.Fatalf("explosion did not push the cow away: %v", cow.Velocity())
		}
	})
}
