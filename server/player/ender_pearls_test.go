package player

import (
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestEnderPearlsVanishOnOwnerDeath(t *testing.T) {
	for _, vanish := range []bool{true, false} {
		w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: entity.DefaultRegistry}.New()
		<-w.Exec(func(tx *world.Tx) {
			if err := tx.World().SetGameRule("ender_pearls_vanish_on_death", vanish); err != nil {
				t.Fatal(err)
			}
			p := tx.AddEntity(world.EntitySpawnOpts{Position: mgl64.Vec3{0, 64, 0}}.New(Type, Config{Name: "Thrower", GameMode: world.GameModeSurvival})).(*Player)
			pearl := tx.AddEntity(entity.NewEnderPearl(world.EntitySpawnOpts{Position: mgl64.Vec3{0, 80, 0}}, p))
			p.Hurt(1000, entity.VoidDamageSource{})
			_, alive := pearl.H().Entity(tx)
			if alive == vanish {
				t.Errorf("ender_pearls_vanish_on_death=%v: pearl alive=%v after owner death", vanish, alive)
			}
		})
		_ = w.Close()
	}
}
