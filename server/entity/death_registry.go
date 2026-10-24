package entity

import (
	"sync"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// entityDeath is what is remembered of a mob that died. Java keeps a dying
// mob in the world for the twenty ticks of its death animation, in which the
// brains that remember it see it dead, while a mob here leaves the world as
// it dies.
type entityDeath struct {
	typ    world.EntityType
	pos    mgl64.Vec3
	player bool
	w      *world.World
	tick   int64
}

// deathTime is LivingEntity.DEATH_DURATION, how long a dying mob stays.
const deathTime = 20

var recentDeaths = struct {
	sync.Mutex
	m map[uuid.UUID]entityDeath
}{m: map[uuid.UUID]entityDeath{}}

// recordDeath remembers a mob that died for the twenty ticks it would take
// to leave the world.
func recordDeath(tx *world.Tx, e world.Entity) {
	now := tx.CurrentTick()
	recentDeaths.Lock()
	defer recentDeaths.Unlock()
	for id, d := range recentDeaths.m {
		if d.w == tx.World() && now-d.tick > deathTime || d.w != tx.World() && len(recentDeaths.m) > 1024 {
			delete(recentDeaths.m, id)
		}
	}
	recentDeaths.m[e.H().UUID()] = entityDeath{typ: e.H().Type(), pos: e.Position(), player: isPlayerEntity(e), w: tx.World(), tick: now}
}

// recentDeath returns how the entity with the UUID passed died, if it died
// in the last twenty ticks.
func recentDeath(tx *world.Tx, id uuid.UUID) (entityDeath, bool) {
	recentDeaths.Lock()
	defer recentDeaths.Unlock()
	d, ok := recentDeaths.m[id]
	if !ok || d.w != tx.World() || tx.CurrentTick()-d.tick > deathTime {
		return entityDeath{}, false
	}
	return d, true
}
