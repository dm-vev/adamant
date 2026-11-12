package world

import (
	"errors"
	"slices"
	"sync/atomic"

	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/goleveldb/leveldb"
)

// chunkStore is the chunks currently loaded, the last one read, and what waits
// for them.
type chunkStore struct {
	// chunks holds a cache of chunks currently loaded. These chunks are cleared
	// from this map after some time of not being used.
	chunks map[ChunkPos]*Column
	// lastChunk caches the settled column most recently read at
	// lastChunkPos, as ServerChunkCache keeps its last chunks. It is cleared
	// whenever the column at a position is replaced or removed.
	lastChunkPos  ChunkPos
	lastChunk     *Column
	chunkRequests map[ChunkPos][]chunkRequest
	// unsettled holds the columns in chunks that were not settled yet when
	// last seen, so that a region phase may settle them first.
	unsettled map[ChunkPos]*Column
	regions   regionState
	// workerLocals holds the values Tx.Local and ReadView.Local keep for the
	// world owner, first, and the other workers of parallel phases.
	workerLocals []map[any]any
	// chunkCount tracks the size of chunks so callers outside the world
	// goroutine can read the count without racing map mutations.
	chunkCount atomic.Int64
}

type chunkCallback func(tx *Tx, col *Column)

// chunkRequest is a callback waiting for a column, and the loader that asked
// for it, if one did: a loader that stops wanting the column takes its
// requests back, see releaseChunkRequests.
type chunkRequest struct {
	loader   *Loader
	callback chunkCallback
}

// LoadedChunkCount returns the number of chunks currently kept in memory by the
// world.
func (w *World) LoadedChunkCount() int {
	if w == nil {
		return 0
	}
	return int(w.chunkCount.Load())
}

// loadedChunk returns chunk & true only if chunk at position passed is loaded.
func (w *World) loadedChunk(pos ChunkPos) (*Column, bool) {
	c, ok := w.chunks[pos]
	return c, ok && c.Ready() && c.lightReady.Load()
}

// waitColumnReady waits for the column at pos to finish generating. Its
// generation first moves ahead of the queued chunks nothing waits for yet,
// so the world goroutine waits for one chunk rather than the whole backlog.
func (w *World) waitColumnReady(pos ChunkPos, c *Column) {
	if c.Ready() {
		return
	}
	if w.generatorQueue != nil {
		w.generatorQueue.prioritize(pos)
	}
	c.waitReady()
}

func (w *World) chunkLoaded(pos ChunkPos) bool {
	if c, ok := w.chunks[pos]; ok {
		return c.Ready()
	}
	return false
}

// loadChunk loads or generates a chunk (column) for the given position.
//
// Behavior summary:
//  1. If the chunk exists in persistent storage, load it and mark as ready.
//  2. If not found, create a new column and generate it asynchronously.
//  3. If an unexpected error occurs, return an empty ready column to prevent blocking.
//
// This function guarantees that the returned *Column will eventually become ready,
// even if generation is canceled due to shutdown.
func (w *World) loadChunk(pos ChunkPos) (*Column, error) {
	// Attempt to load the column staged as it unloaded, or else from the
	// persistent provider (e.g. LevelDB), after a save of it still waiting
	// for the background saver. A column taken from staging is written by
	// the next capture as it is then.
	column, staged, err := w.readColumn(pos)

	switch {
	case err == nil:
		w.stats.ChunksRead.Inc()
		// Case 1: Column successfully loaded from persistent storage. A
		// world ticking on its own goroutine fills its light on another and
		// publishes it once it did, as it does a column generated.
		async := !w.conf.Synchronous
		col := w.columnFrom(column, pos, !async)
		col.complete = true
		col.modified.Store(col.modified.Load() || staged)
		col.fillingLight = async
		if !async {
			col.fillLight(pos)
		}
		if _, ok := w.chunks[pos]; !ok {
			w.chunkCount.Add(1)
		} else {
			w.columnLeft(pos)
		}
		w.placeColumn(pos, col)
		w.markUnsettled(pos, col)
		if w.lastChunkPos == pos {
			w.lastChunk = nil
		}
		w.indexPOIChunk(pos, col)
		w.indexCircuitChunk(pos, col)
		w.reconcileMissingBlockStates(pos)

		// Register all entities contained in this column into the world.
		w.set.Lock()
		currentTick := w.currentTickLocked()
		w.set.Unlock()
		for _, e := range col.Entities {
			e.setAndUnlockWorld(w)
			if _, ok := w.entities[e]; !ok {
				w.entityCount.Add(1)
			}
			state := &entityState{
				pos:               pos,
				lastTick:          currentTick,
				lastProcessedTick: currentTick,
				isItem:            e.t.EncodeEntity() == "minecraft:item",
			}
			w.entities[e] = state
			w.trackPlayer(e)
			w.entitiesByUUID[e.id] = e
			w.entitiesByUniqueID[e.UniqueID()] = e
			e.state = state
			w.resolveEntityRiding(e)
			e.markWorldReady(w)
		}

		if len(col.Entities) > 0 {
			w.addEntityColumn(pos, col)
		}
		if async {
			w.fillLightAsync(pos, col)
		}
		return col, nil

	case errors.Is(err, leveldb.ErrNotFound):
		// Case 2: Column not found in storage — needs generation.
		col := newQueuedColumn()
		if _, ok := w.chunks[pos]; !ok {
			w.chunkCount.Add(1)
		} else {
			w.columnLeft(pos)
		}
		w.placeColumn(pos, col)
		w.markUnsettled(pos, col)
		if w.lastChunkPos == pos {
			w.lastChunk = nil
		}

		// Schedule asynchronous generation.
		// generateChunkAsync is shutdown-safe and will mark ready if closing.
		w.generateChunkAsync(pos, col)

		return col, nil

	default:
		// Case 3: Unexpected error occurred (I/O failure, corruption, etc.)
		// To avoid deadlocks, return a ready empty column and the error.
		// The column is never saved, so that it does not overwrite the
		// column in the provider; loading the position again once it was
		// unloaded reads it from the provider again.
		w.stats.ChunkLoadErrors.Inc()
		col := newColumn(chunk.New(w.conf.Blocks, w.Range()))
		col.loadErr = err
		col.fillLight(pos)
		col.markReady()
		// Keep the placeholder column tracked so callers don't mutate an untracked chunk on errors.
		if _, ok := w.chunks[pos]; !ok {
			w.chunkCount.Add(1)
		} else {
			w.columnLeft(pos)
		}
		w.placeColumn(pos, col)
		w.markUnsettled(pos, col)
		if w.lastChunkPos == pos {
			w.lastChunk = nil
		}
		return col, err
	}
}

// readColumn returns the column at pos for loadChunk: the one staged as it
// unloaded, or the one read through the commit still holding its last
// save, whether staged says so, which the provider may lack, or else the
// one read from the provider or the one a read ahead brought. An error is that of the provider, which wraps leveldb.ErrNotFound
// for a column it has none of.
func (w *World) readColumn(pos ChunkPos) (column *chunk.Column, staged bool, err error) {
	if w.saver != nil {
		var from Provider
		if column, staged, from, err = w.saver.takeStaged(pos); staged {
			return column, true, err
		}
		if from != nil {
			// The commit may still fail: the column is saved whole by the
			// next capture, as one taken from staging is.
			column, err = from.LoadColumn(pos, w.conf.Dim)
			return column, true, err
		}
	}
	if column, ahead, err := w.takePrefetched(pos); ahead {
		return column, false, err
	}
	column, err = w.conf.Provider.LoadColumn(pos, w.conf.Dim)
	return column, false, err
}

// loadChunkAsync loads or generates the chunk at pos in the background,
// calling callback once ready. It returns false if it could not be scheduled.
func (w *World) loadChunkAsync(tx *Tx, pos ChunkPos, callback chunkCallback) bool {
	return w.loadChunkFor(tx, pos, nil, callback)
}

// loadChunkFor is loadChunkAsync for a request of the loader l, which
// releaseChunkRequests takes back.
func (w *World) loadChunkFor(tx *Tx, pos ChunkPos, l *Loader, callback chunkCallback) bool {
	if w.closed.Load() {
		return false
	}
	request := chunkRequest{loader: l, callback: callback}
	if c, ok := w.chunks[pos]; ok {
		w.chunkRequests[pos] = append(w.chunkRequests[pos], request)
		if c.Ready() {
			c.ensureLight(w, pos)
			w.finishChunkRequest(tx, pos, c)
		}
		return true
	}

	w.chunkRequests[pos] = append(w.chunkRequests[pos], request)
	c, err := w.loadChunk(pos)
	if err != nil {
		w.log.Error("load chunk: "+err.Error(), "X", pos[0], "Z", pos[1])
	}
	if c.Ready() {
		c.ensureLight(w, pos)
		w.finishChunkRequest(tx, pos, c)
	}
	return true
}

// finishChunkRequest publishes a ready chunk to all loaders waiting for it.
// It must run on the world owner.
func (w *World) finishChunkRequest(tx *Tx, pos ChunkPos, c *Column) {
	if !c.Ready() {
		return
	}
	if !c.poisIndexed {
		w.indexPOIChunk(pos, c)
	}
	if !c.circuitsIndexed {
		w.indexCircuitChunk(pos, c)
	}
	w.finishGeneration(tx, c)
	requests := w.chunkRequests[pos]
	if len(requests) == 0 {
		return
	}
	delete(w.chunkRequests, pos)
	if w.closed.Load() {
		return
	}
	for _, request := range requests {
		callback := request.callback
		tx.Defer(func(tx *Tx) {
			if w.closed.Load() {
				return
			}
			callback(tx, c)
		})
	}
}

// CollectGarbage closes chunks that have no viewers or loaders and returns the number of
// chunks, entities and block entities that were removed as a result.
func (w *World) CollectGarbage(tx *Tx) (chunksCollected, entitiesCollected, blockEntitiesCollected int) {
	if w == nil {
		return 0, 0, 0
	}
	for pos, c := range w.chunks {
		if w.retainedByForcedChunk(pos) {
			continue
		}
		if !c.Ready() {
			w.pruneChunkRequests(pos)
			if w.abandonColumn(pos, c) {
				chunksCollected++
			}
			continue
		}
		if len(w.chunkRequests[pos]) != 0 || len(c.viewers) != 0 || len(c.loaders) != 0 {
			continue
		}
		chunksCollected++
		entitiesCollected += len(c.Entities)
		blockEntitiesCollected += len(c.BlockEntities)
		w.closeChunk(tx, pos, c)
	}
	return
}

// closeUnusedChunks closes chunks currently not in use by any viewer or loader.
func (w *World) closeUnusedChunks(tx *Tx) {
	w.CollectGarbage(tx)
}

// releaseChunkRequests takes back the requests the loader l made for the
// columns at positions, which it no longer wants, and abandons those of the
// columns nothing else wants that are still queued for generation. It must
// be called from the owner.
func (w *World) releaseChunkRequests(l *Loader, positions []ChunkPos) {
	for _, pos := range positions {
		requests := w.chunkRequests[pos]
		kept := slices.DeleteFunc(requests, func(r chunkRequest) bool { return r.loader == l })
		if len(kept) != len(requests) {
			if len(kept) == 0 {
				delete(w.chunkRequests, pos)
			} else {
				w.chunkRequests[pos] = kept
			}
		}
		if c, ok := w.chunks[pos]; ok && !c.Ready() && !w.retainedByForcedChunk(pos) {
			w.abandonColumn(pos, c)
		}
	}
}

// pruneChunkRequests drops the requests for the column at pos of the
// loaders that no longer await it.
func (w *World) pruneChunkRequests(pos ChunkPos) {
	requests := w.chunkRequests[pos]
	kept := slices.DeleteFunc(requests, func(r chunkRequest) bool {
		return r.loader != nil && !r.loader.awaits(w, pos)
	})
	if len(kept) == len(requests) {
		return
	}
	if len(kept) == 0 {
		delete(w.chunkRequests, pos)
		return
	}
	w.chunkRequests[pos] = kept
}

// abandonColumn removes the column at pos, still queued for generation, if
// nothing wants it: no request waits for it and it holds no entity. The
// worker that takes its task skips it, and the column is generated again if
// it is asked for again. It reports whether the column was removed. A column
// a worker started generating is left to finish, and unloads once ready.
func (w *World) abandonColumn(pos ChunkPos, c *Column) bool {
	if len(w.chunkRequests[pos]) != 0 || len(c.Entities) != 0 || len(c.viewers) != 0 || len(c.loaders) != 0 {
		return false
	}
	if !c.abandonGeneration() {
		return false
	}
	delete(w.chunks, pos)
	delete(w.unsettled, pos)
	delete(w.pendingLight, pos)
	if w.lastChunkPos == pos {
		w.lastChunk = nil
	}
	w.chunkCount.Add(-1)
	w.stats.ChunksUnloaded.Inc()
	w.columnLeft(pos)
	// Nothing reads the column any more; whatever might wait for it does
	// not wait for ever.
	c.markReady()
	return true
}
