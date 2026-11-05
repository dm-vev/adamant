package world

import (
	"iter"
	"slices"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/google/uuid"
)

type entityState struct {
	pos               ChunkPos
	ent               Entity
	lastTick          int64
	lastProcessedTick int64
	lastPostBlockTick int64
	// firstTickPending distinguishes the spawn-time processed marker from an
	// actual entity tick. A block may explicitly admit a new actor this tick.
	firstTickPending bool
	// nextPassiveTick is the next scheduled tick at which the entity should receive
	// maintenance updates such as ageing and fire decay while it is outside of the
	// active simulation range.
	nextPassiveTick int64
	// isItem caches whether the entity type is a dropped item (minecraft:item).
	// This avoids calling EncodeEntity() for every entity on every tick.
	isItem bool
	// isTicker caches whether the entity implements TickerEntity so we can avoid
	// repeating expensive type assertions in the hot tick path.
	isTicker      bool
	tickerChecked bool
	ticker        TickerEntity
	// faults counts the panics of the entity, which no longer ticks once
	// quarantined.
	faults      faultCount
	quarantined bool
	// binds records whether ent rebinds to each transaction it is used in,
	// so that it may be handed out again instead of a fresh view.
	binds            bool
	binder           interface{ BindTransaction(*Tx) }
	postBlockTicker  interface{ TickAfterBlockUpdates(*Tx, int64) }
	preBlockTicker   interface{ TickBeforeBlockUpdates(*Tx, int64) }
	lastPreBlockTick int64
	// bound is the transaction binder was bound to last.
	bound *Tx
}

func (s *entityState) entity(tx *Tx, handle *EntityHandle) Entity {
	if s == nil {
		return nil
	}
	if s.ent == nil {
		s.ent = handle.mustEntity(tx)
		s.binder, s.binds = s.ent.(interface{ BindTransaction(*Tx) })
	}
	if !s.tickerChecked {
		if ticker, ok := s.ent.(TickerEntity); ok {
			s.ticker = ticker
			s.isTicker = true
		} else {
			s.ticker = nil
			s.isTicker = false
		}
		s.tickerChecked = true
		s.postBlockTicker, _ = s.ent.(interface{ TickAfterBlockUpdates(*Tx, int64) })
		s.preBlockTicker, _ = s.ent.(interface{ TickBeforeBlockUpdates(*Tx, int64) })
	}
	if s.binds {
		s.bind(tx)
	}
	return s.ent
}

// bind binds the entity of s to tx. A synchronous World runs a transaction
// started in another at once, as EntityHandle.Do, World.Exec and Call do
// from a transaction: if tx is such a transaction and the entity was bound
// to the one it runs in, tx binds it back to that one as it closes, so that
// the transaction it ran in goes on using the entity. It would otherwise be
// left bound to tx, which ended.
func (s *entityState) bind(tx *Tx) {
	if prev := s.bound; prev != tx {
		if prev != nil && prev.nests(tx) {
			tx.rebound = append(tx.rebound, rebinding{s: s, prev: prev})
		}
		s.bound = tx
	}
	s.binder.BindTransaction(tx)
}

// EntityCount returns the number of entities tracked by the world.
func (w *World) EntityCount() int {
	if w == nil {
		return 0
	}
	return int(w.entityCount.Load())
}

// addEntity adds an EntityHandle to a World. The Entity will be visible to all
// viewers of the World that have the chunk at the EntityHandle's position. If
// the chunk that the EntityHandle is in is not yet loaded, it will first be
// loaded. addEntity panics if the EntityHandle is already in a world.
// addEntity returns the Entity created by the EntityHandle.
func (w *World) addEntity(tx *Tx, handle *EntityHandle) Entity {
	tx.lockShared()
	defer tx.unlockShared()
	handle.setAndUnlockWorld(w)
	// An entity transferred is in a world again.
	handle.endTransfer()
	pos := chunkPosFromVec3(handle.data.Pos)
	w.set.Lock()
	currentTick := w.currentTickLocked()
	w.set.Unlock()
	state := &entityState{
		pos:               pos,
		lastTick:          currentTick,
		lastProcessedTick: currentTick,
		firstTickPending:  true,
		isItem:            handle.t.EncodeEntity() == "minecraft:item",
	}
	if _, ok := w.entities[handle]; !ok {
		w.entityCount.Add(1)
	}
	w.entities[handle] = state
	w.trackPlayer(handle)
	w.entitiesByUUID[handle.id] = handle
	w.entitiesByUniqueID[handle.UniqueID()] = handle
	handle.state = state
	if tx.region == nil {
		w.resolveEntityRiding(handle)
	} else if handle.pendingVehicle != uuid.Nil || len(w.pendingRiders[handle.id]) > 0 {
		// Riders waiting for the entity may be in other regions.
		tx.deferToOwner(func(*Tx) {
			if handle.state == state {
				w.resolveEntityRiding(handle)
			}
		})
	}

	if r := tx.region; r != nil {
		if _, ok := r.loaded(w, pos); !ok {
			// The column is not loaded or another region's: the owner puts the
			// entity in it after the phase. Until then no column lists it.
			e := state.entity(tx, handle)
			handle.finalizeSpawn(e)
			w.Handler().HandleEntitySpawn(tx, e)
			handle.markWorldReady(w)
			r.deferWork(func(tx *Tx) {
				if handle.state != state || handle.w != w {
					return
				}
				c := w.placeEntity(tx, handle, state)
				e := state.entity(tx, handle)
				for v := range c.viewers {
					showEntity(e, v)
				}
			}, handle)
			return e
		}
	}
	c := w.placeEntity(tx, handle, state)
	e := state.entity(tx, handle)
	handle.finalizeSpawn(e)
	for v := range c.viewers {
		// Show the entity to all viewers in the chunk of the entity.
		showEntity(e, v)
	}
	w.Handler().HandleEntitySpawn(tx, e)
	handle.markWorldReady(w)
	return e
}

// placeEntity lists the entity of handle, just added to the world, in the
// column at its chunk, state.pos, loading the column if needed, and returns
// the column.
func (w *World) placeEntity(tx *Tx, handle *EntityHandle, state *entityState) *Column {
	// Entity membership is owner-only even while the terrain worker fills the
	// column. Like Java's entity sections, attaching an entity must not await
	// terrain generation; block reads retain their normal readiness checks.
	pos := state.pos
	c, loaded := w.chunks[pos]
	if !loaded {
		var err error
		if c, err = w.loadChunk(pos); err != nil {
			w.log.Error("load chunk: "+err.Error(), "X", pos[0], "Z", pos[1])
		}
	}
	if c.Ready() && tx.region == nil {
		c = tx.chunk(pos)
	}
	c.addEntity(handle)
	if c.Ready() {
		// The worker owns this flag before publication and dirties it itself.
		c.modified = true
	}
	w.addEntityColumn(pos, c)
	return c
}

// removeEntity removes an Entity from the World that is currently present in
// it. Any viewers of the Entity will no longer be able to see it.
// removeEntity returns the EntityHandle of the Entity. After removing an Entity
// from the World, the Entity is no longer usable.
func (w *World) removeEntity(e Entity, tx *Tx) *EntityHandle {
	tx.lockShared()
	defer tx.unlockShared()
	handle := e.H()
	state, found := w.entities[handle]
	if !found {
		// The entity currently isn't in this world.
		return nil
	}
	pos := state.pos
	w.Handler().HandleEntityDespawn(tx, e)

	// The entity's chunk is loaded, though it may still be generating if the
	// entity was teleported there. In a region phase, an entity whose column
	// is not the region's was added in the phase and is listed in none yet.
	c, loaded := tx.loadedColumn(pos)
	if loaded {
		if c.removeEntity(handle) && c.Ready() {
			c.modified = true
		}
		if len(c.Entities) == 0 {
			w.removeEntityColumn(pos)
		}
	}

	tx.deferToOwner(func(*Tx) { w.removeEntityFromViewLayers(e) })
	if loaded {
		for v := range c.viewers {
			v.HideEntity(e)
		}
	}
	w.removePendingRider(handle)
	w.forgetEntity(handle, state)
	delete(w.entities, handle)
	w.untrackPlayer(handle)
	delete(w.entitiesByUUID, handle.id)
	delete(w.entitiesByUniqueID, handle.UniqueID())
	w.entityCount.Add(-1)
	handle.state = nil
	handle.unsetAndLockWorld()
	return handle
}

// removeEntityFromViewLayers removes stale overrides for despawned entities. Entities that own a ViewLayer,
// such as players, are skipped because they may be removed temporarily when respawning or changing worlds.
func (w *World) removeEntityFromViewLayers(e Entity) {
	if _, ok := e.(viewLayerViewer); ok {
		return
	}
	viewers, _ := w.allViewers()
	for _, viewer := range viewers {
		v, ok := viewer.(viewLayerViewer)
		if !ok || v.ViewLayer() == nil {
			continue
		}
		v.ViewLayer().remove(e)
	}
	w.releaseViewers(viewers)
}

// entitiesWithin returns an iterator that yields all entities contained within
// the cube.BBox passed.
func (w *World) entitiesWithin(tx *Tx, box cube.BBox) iter.Seq[Entity] {
	return func(yield func(Entity) bool) {
		minPos, maxPos := chunkPosFromVec3(box.Min()), chunkPosFromVec3(box.Max())

		for x := minPos[0]; x <= maxPos[0]; x++ {
			for z := minPos[1]; z <= maxPos[1]; z++ {
				c, ok := tx.loadedColumn(ChunkPos{x, z})
				if !ok || len(c.Entities) == 0 {
					// The chunk wasn't loaded, so there are no entities here.
					continue
				}
				more := c.iterateEntities(func(handle *EntityHandle) bool {
					if !box.Vec3Within(handle.data.Pos) {
						return true
					}
					// An entity removed while the column is walked has no
					// state any more.
					state := handle.state
					if state == nil {
						return true
					}
					return yield(state.entity(tx, handle))
				})
				if !more {
					return
				}
			}
		}
	}
}

// entitiesOfTypesWithin yields the entities entitiesWithin yields for box
// whose type encodes to one of types, in the same order, without opening
// the others.
func (w *World) entitiesOfTypesWithin(tx *Tx, box cube.BBox, types []string) iter.Seq[Entity] {
	return func(yield func(Entity) bool) {
		minPos, maxPos := chunkPosFromVec3(box.Min()), chunkPosFromVec3(box.Max())
		for x := minPos[0]; x <= maxPos[0]; x++ {
			for z := minPos[1]; z <= maxPos[1]; z++ {
				c, ok := tx.loadedColumn(ChunkPos{x, z})
				if !ok || len(c.Entities) == 0 {
					continue
				}
				more := c.iterateEntities(func(handle *EntityHandle) bool {
					if !box.Vec3Within(handle.data.Pos) || !slices.Contains(types, handle.t.EncodeEntity()) {
						return true
					}
					state := handle.state
					if state == nil {
						return true
					}
					return yield(state.entity(tx, handle))
				})
				if !more {
					return
				}
			}
		}
	}
}

// tickingEntities yields the entities of the columns tickEntities ticks
// actively, in its order.
func (w *World) tickingEntities(tx *Tx) iter.Seq[Entity] {
	return func(yield func(Entity) bool) {
		refs := w.entityColumns
		if r := tx.region; r != nil {
			refs = r.refs
		}
		for _, ref := range refs {
			col := ref.col
			if col == nil || len(col.Entities) == 0 || !col.Ready() || !w.entitiesTick(col) || !w.entityTickingReady(ref.pos) {
				continue
			}
			more := col.iterateEntities(func(handle *EntityHandle) bool {
				state := handle.state
				if state == nil {
					return true
				}
				return yield(state.entity(tx, handle))
			})
			if !more {
				return
			}
		}
	}
}

// allEntities returns an iterator that yields all entities in the World.
func (w *World) allEntities(tx *Tx) iter.Seq[Entity] {
	return w.entitiesOfType(tx, nil)
}

// entitiesOfType returns an iterator that yields the entities in the World
// whose type match returns true for, or all of them if match is nil.
func (w *World) entitiesOfType(tx *Tx, match func(EntityType) bool) iter.Seq[Entity] {
	return func(yield func(Entity) bool) {
		// The handles are listed first, so entities may join or leave the
		// world while they are walked.
		tx.lockShared()
		handles := make([]*EntityHandle, 0, len(w.entities))
		for handle := range w.entities {
			if match != nil && !match(handle.t) {
				continue
			}
			if r := tx.region; r == nil || r.ownsEntity(w, handle) {
				handles = append(handles, handle)
			}
		}
		tx.unlockShared()
		for _, handle := range handles {
			state := handle.state
			if state == nil || handle.w != w {
				continue
			}
			if ent := state.entity(tx, handle); ent != nil {
				if !yield(ent) {
					return
				}
			}
		}
	}
}

// allPlayers returns an iterator that yields all player entities in the World.
func (w *World) allPlayers(tx *Tx) iter.Seq[Entity] {
	return func(yield func(Entity) bool) {
		// A snapshot of the players, as yielding may add or remove some.
		tx.lockShared()
		handles := w.playerList
		if r := tx.region; r != nil {
			handles = make([]*EntityHandle, 0, len(w.playerList))
			for _, handle := range w.playerList {
				if r.ownsEntity(w, handle) {
					handles = append(handles, handle)
				}
			}
		}
		tx.unlockShared()
		for _, handle := range handles {
			state := handle.state
			if state == nil || handle.w != w {
				continue
			}
			if ent := state.entity(tx, handle); ent != nil {
				if !yield(ent) {
					return
				}
			}
		}
	}
}

// trackPlayer records a handle just put in entities if it is a player.
func (w *World) trackPlayer(handle *EntityHandle) {
	if _, ok := w.players[handle]; ok || handle.t.EncodeEntity() != "minecraft:player" {
		return
	}
	w.players[handle] = struct{}{}
	w.playerList = append(slices.Clip(w.playerList), handle)
	w.playerMoves.Add(1)
}

// untrackPlayer forgets a handle leaving entities if it is a player.
func (w *World) untrackPlayer(handle *EntityHandle) {
	if _, ok := w.players[handle]; !ok {
		return
	}
	delete(w.players, handle)
	i := slices.Index(w.playerList, handle)
	w.playerList = slices.Concat(w.playerList[:i], w.playerList[i+1:])
	w.playerMoves.Add(1)
}

func (w *World) addEntityColumn(pos ChunkPos, col *Column) {
	if col == nil || len(col.Entities) == 0 {
		w.removeEntityColumn(pos)
		return
	}
	if w.entityColumnIndex == nil {
		w.entityColumnIndex = make(map[ChunkPos]int)
	}
	if idx, ok := w.entityColumnIndex[pos]; ok {
		w.entityColumns[idx].col = col
		return
	}
	w.entityColumns = append(w.entityColumns, columnRef{pos: pos, col: col})
	w.entityColumnIndex[pos] = len(w.entityColumns) - 1
}

func (w *World) removeEntityColumn(pos ChunkPos) {
	if len(w.entityColumns) == 0 {
		return
	}
	idx, ok := w.entityColumnIndex[pos]
	if !ok {
		return
	}
	last := len(w.entityColumns) - 1
	if idx != last {
		w.entityColumns[idx] = w.entityColumns[last]
		w.entityColumnIndex[w.entityColumns[idx].pos] = idx
	}
	w.entityColumns = w.entityColumns[:last]
	delete(w.entityColumnIndex, pos)
}
