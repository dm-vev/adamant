package entity

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestEveryMobPersistsItsEffects guards against mobs whose effects are lost
// on saving because their behaviour does not expose its effect manager.
func TestEveryMobPersistsItsEffects(t *testing.T) {
	w := world.Config{Entities: DefaultRegistry, Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		for _, typ := range DefaultRegistry.Types() {
			h := spawnMob(typ.EncodeEntity(), world.EntitySpawnOpts{Position: mgl64.Vec3{0, 100, 0}, SpawnFinalized: true})
			if h == nil {
				continue
			}
			l, ok := tx.AddEntity(h).(Living)
			if !ok {
				continue
			}
			// Ender dragons ignore status effects.
			if l.AddEffect(effect.New(effect.Speed, 1, time.Second)); len(l.Effects()) == 0 {
				continue
			}
			b, ok := l.(interface{ Behaviour() Behaviour })
			if !ok {
				t.Errorf("%s keeps its effects outside a behaviour", typ.EncodeEntity())
				continue
			}
			if _, ok := b.Behaviour().(effectHolder); !ok {
				t.Errorf("%s does not persist its effects", typ.EncodeEntity())
			}
		}
	})
}
