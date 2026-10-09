package world

import (
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/google/uuid"
)

// JavaRandom returns the world-owned Java LegacyRandomSource stream. The caller
// must not retain or use it outside this transaction. Like Level.random, it is
// runtime state rather than a generation-seed stream or persisted world data.
func (tx *Tx) JavaRandom() *java26.Random {
	w := tx.World()
	if !w.javaRandomReady {
		w.javaRandom = java26.NewLegacyRandom(w.r.Int64())
		w.javaRandomReady = true
	}
	return &w.javaRandom
}

// EntityByUUID resolves a loaded entity without loading chunks or reopening its
// transaction-bound implementation. UUID and Bedrock UniqueID are separate keys.
func (tx *Tx) EntityByUUID(id uuid.UUID) (Entity, bool) {
	w := tx.World()
	handle, ok := w.entitiesByUUID[id]
	if !ok {
		return nil, false
	}
	return w.entities[handle].entity(tx, handle), true
}

// EntityByUniqueID resolves a loaded entity by its persisted Bedrock actor ID.
func (tx *Tx) EntityByUniqueID(id int64) (Entity, bool) {
	w := tx.World()
	handle, ok := w.entitiesByUniqueID[id]
	if !ok {
		return nil, false
	}
	return w.entities[handle].entity(tx, handle), true
}

// CreakingActive evaluates vanilla 26.3's gameplay/creaking_active attribute.
// Its default is false; the Overworld day timeline ORs in true at 12600 and
// false at 23401 in its 24000-tick period. Neither other vanilla dimension
// attaches that timeline or overrides this attribute.
func (w *World) CreakingActive() bool {
	if w == nil || w.Dimension() != Overworld {
		return false
	}
	tick := w.Time() % 24000
	if tick < 0 {
		tick += 24000
	}
	return tick >= 12600 && tick < 23401
}

// MonsterSpawningEnabled evaluates both Java rules used by
// ServerLevel.isSpawningMonsters. Difficulty is a separate spawning check.
func (w *World) MonsterSpawningEnabled() bool {
	if w == nil {
		return false
	}
	w.set.Lock()
	defer w.set.Unlock()
	return w.set.SpawnMobs && w.set.SpawnMonsters
}

// SetSpawnMobs changes Java's spawn_mobs rule, persisted as Bedrock domobspawning.
func (w *World) SetSpawnMobs(enabled bool) {
	w.set.Lock()
	defer w.set.Unlock()
	w.set.SpawnMobs = enabled
}

// SetSpawnMonsters changes the independent Java spawn_monsters rule.
func (w *World) SetSpawnMonsters(enabled bool) {
	w.set.Lock()
	defer w.set.Unlock()
	w.set.SpawnMonsters = enabled
}

// SpawnWardensEnabled reports Java's independent spawn_wardens gamerule.
func (w *World) SpawnWardensEnabled() bool {
	if w == nil {
		return false
	}
	w.set.Lock()
	defer w.set.Unlock()
	return w.set.SpawnWardens
}

// SetSpawnWardens changes Java's spawn_wardens gamerule.
func (w *World) SetSpawnWardens(enabled bool) {
	w.set.Lock()
	defer w.set.Unlock()
	w.set.SpawnWardens = enabled
}

// SetWaterSourceConversion controls Java's water_source_conversion rule.
func (w *World) SetWaterSourceConversion(enabled bool) {
	w.set.Lock()
	defer w.set.Unlock()
	w.set.WaterSourceConversion = enabled
}

// SetLavaSourceConversion controls Java's lava_source_conversion rule.
func (w *World) SetLavaSourceConversion(enabled bool) {
	w.set.Lock()
	defer w.set.Unlock()
	w.set.LavaSourceConversion = enabled
}
