package entity

import (
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"hash/fnv"
)

// randomOf returns the random stream an entity draws from: the stream of its
// mob, Java's entity.random, or the Level.random of the world for an entity
// that is no mob.
func randomOf(tx *world.Tx, e world.Entity) *vanilla.Random {
	if m, ok := mobOf(e); ok {
		return m.Random()
	}
	return tx.RegionRandom()
}

// randInt returns a random int in [0, n) from r, as RandomSource.nextInt(n).
func randInt(r *vanilla.Random, n int) int { return int(r.NextInt(int32(n))) }

// randIntRange returns a random int in [lo, hi] from r, as Mth.nextInt.
func randIntRange(r *vanilla.Random, lo, hi int) int { return lo + randInt(r, hi-lo+1) }

// randomParent returns either of the two values with equal chance, drawing
// from r.
func randomParent[T any](r *vanilla.Random, a, b T) T {
	if r.NextBoolean() {
		return a
	}
	return b
}

// entRandom returns the random stream of the entity e, the stream of its mob
// if the behaviour of e has an AI, and the Level.random of the world of tx
// otherwise.
func entRandom(tx *world.Tx, e *Ent) *vanilla.Random {
	if b, ok := e.Behaviour().(interface{ mobAI() *MobAI }); ok {
		if ai := b.mobAI(); ai != nil {
			return ai.randomSource(tx, e)
		}
	}
	return tx.RegionRandom()
}

// openRandom returns the random stream of the state of an entity that is
// opened for the first time. Its seed is drawn from the Level.random of the
// world of tx, so that the same world seed gives the same streams; without a
// transaction the UUID of the entity stands in.
func openRandom(tx *world.Tx, h *world.EntityHandle) vanilla.Random {
	if tx != nil {
		return vanilla.NewLegacyRandom(tx.RegionRandom().NextLong())
	}
	id := h.UUID()
	hash := fnv.New64a()
	_, _ = hash.Write(id[:])
	return vanilla.NewLegacyRandom(int64(hash.Sum64()))
}
