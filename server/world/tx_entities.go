package world

import (
	"iter"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/go-gl/mathgl/mgl64"
)

// EntityTicking reports whether the entities of the loaded chunk at pos tick,
// as Java's ServerLevel.canSpawnEntitiesInChunk asks of a chunk mobs spawn
// in.
func (tx *Tx) EntityTicking(pos ChunkPos) bool {
	c, ok := tx.loadedColumn(pos)
	if !ok || tx.region == nil && !c.Ready() || tx.region != nil && !c.settled {
		return false
	}
	w := tx.World()
	return w.entitiesTick(c) && w.columnEntityTickingReady(pos, c)
}

// AddEntity adds an EntityHandle to a World. The Entity will be visible to all
// viewers of the World that have the chunk at the EntityHandle's position. If
// the chunk that the EntityHandle is in is not yet loaded, loading or generation
// is started without waiting for terrain. Block reads still wait for generation.
// AddEntity panics if the EntityHandle is already in a world.
// AddEntity returns the Entity created by the EntityHandle.
func (tx *Tx) AddEntity(e *EntityHandle) Entity {
	return tx.World().addEntity(tx, e)
}

// RemoveEntity removes an Entity from the World that is currently present in
// it. Any viewers of the Entity will no longer be able to see it.
// RemoveEntity returns the EntityHandle of the Entity. After removing an Entity
// from the World, the Entity is no longer usable.
func (tx *Tx) RemoveEntity(e Entity) *EntityHandle {
	return tx.World().removeEntity(e, tx)
}

// EntitiesWithin returns an iterator that yields all entities contained within
// the cube.BBox passed.
func (tx *Tx) EntitiesWithin(box cube.BBox) iter.Seq[Entity] {
	return tx.World().entitiesWithin(tx, box)
}

// EntityChunk returns the chunk whose entity list currently owns e. Movement
// within an entity tick may precede migration to a new chunk, so this can differ
// from the chunk containing e.Position(). The bool is false for removed entities.
func (tx *Tx) EntityChunk(e Entity) (ChunkPos, bool) {
	state, ok := tx.World().entities[e.H()]
	if !ok {
		return ChunkPos{}, false
	}
	return state.pos, true
}

// QueueEntityTick lets an actor created by a block join this tick's entity
// movement after block/circuit updates. It does not repeat an entity's normal
// tick if it has already run, and removed actors are ignored. Call before the
// post-block phase; ordinary entity spawning does not need this operation.
func (tx *Tx) QueueEntityTick(e Entity) {
	w := tx.World()
	state := w.entities[e.H()]
	if state == nil {
		return
	}
	tick := w.CurrentTick()
	if state.firstTickPending {
		if state.isItem && w.blockTickingPass {
			if _, pending := w.pendingBlockTickChunks[state.pos]; !pending {
				return
			}
		}
		state.firstTickPending = false
		state.lastProcessedTick = tick - 1
	}
	if state.lastProcessedTick != tick {
		w.queuedEntityTicks = append(w.queuedEntityTicks, e.H())
	}
}

// Entities returns an iterator that yields all entities in the World.
func (tx *Tx) Entities() iter.Seq[Entity] {
	return tx.World().allEntities(tx)
}

// EntitiesOfType returns an iterator that yields the entities in the World
// whose type match returns true for. Unlike Entities, it does not make the
// others part of the transaction, which spares going through every one.
func (tx *Tx) EntitiesOfType(match func(EntityType) bool) iter.Seq[Entity] {
	return tx.World().entitiesOfType(tx, match)
}

// EntitiesOfTypesWithin returns an iterator that yields the entities
// EntitiesWithin yields for box whose type encodes to one of types, in the
// same order. It skips the others without opening them, as Java's
// getEntitiesOfClass only visits entities of the class asked for.
func (tx *Tx) EntitiesOfTypesWithin(box cube.BBox, types []string) iter.Seq[Entity] {
	return tx.World().entitiesOfTypesWithin(tx, box, types)
}

// TickingEntities returns an iterator that yields the entities whose chunks
// tick this tick, in the order the world ticks them: those in the chunks
// players view, or every chunk of a synchronous world, once the chunks
// around them are generated.
func (tx *Tx) TickingEntities() iter.Seq[Entity] {
	return tx.World().tickingEntities(tx)
}

// Players returns an iterator that yields all player entities in the World.
func (tx *Tx) Players() iter.Seq[Entity] {
	return tx.World().allPlayers(tx)
}

// Viewers returns all viewers viewing the position passed. The returned slice is pooled and must be released
// by calling ReleaseViewers once it is no longer needed.
func (tx *Tx) Viewers(pos mgl64.Vec3) []Viewer {
	return tx.World().viewersOf(tx, pos)
}

// Sleepers returns an iterator that yields all sleeping entities currently added to the World.
func (tx *Tx) Sleepers() iter.Seq[Sleeper] {
	ent := tx.Entities()
	return func(yield func(Sleeper) bool) {
		for e := range ent {
			if sleeper, ok := e.(Sleeper); ok {
				if !yield(sleeper) {
					return
				}
			}
		}
	}
}

// ReleaseViewers returns a slice previously obtained from Viewers back to the internal pool.
func (tx *Tx) ReleaseViewers(viewers []Viewer) {
	tx.World().releaseViewers(viewers)
}
