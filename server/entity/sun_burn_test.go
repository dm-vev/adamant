package entity_test

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/world"
)

// TestDaylightBurnsTheTaggedUndead checks Mob.burnUndead and the
// burn_in_daylight tag: in the sun a zombie horse catches fire, while a
// parched, which the tag leaves out, does not.
func TestDaylightBurnsTheTaggedUndead(t *testing.T) {
	w := happyGhastWorld(t)
	w.SetTime(6000)
	w.StopTime()
	w.SetSpawnMobs(false)
	var horse, parched *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		horse = spawnAnimal(t, tx, "minecraft:zombie_horse", .5, nil)
		parched = spawnAnimal(t, tx, "minecraft:parched", 6.5, map[string]any{"PersistenceRequired": byte(1)})
	})
	burnt := false
	for range 400 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) {
			if e, ok := horse.Entity(tx); ok {
				burnt = burnt || onFire(e)
			} else {
				burnt = true
			}
			if e, ok := parched.Entity(tx); ok && onFire(e) {
				t.Fatal("parched caught fire in daylight")
			}
		})
	}
	if !burnt {
		t.Fatal("zombie horse in daylight did not catch fire in 20 seconds")
	}
}

func onFire(e world.Entity) bool {
	f, ok := e.(interface{ OnFireDuration() time.Duration })
	return ok && f.OnFireDuration() > 0
}
