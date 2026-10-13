package world

import (
	"encoding/json"
	"errors"
	"slices"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/goleveldb/leveldb"
)

const forcedChunksNamespacePrefix = "forced-chunks:"

// ForcedChunks returns a snapshot of the world's forced chunk positions.
func (w *World) ForcedChunks() []ChunkPos {
	if w == nil {
		return nil
	}
	w.loadForcedChunks()
	w.forcedChunksMu.RLock()
	out := make([]ChunkPos, 0, len(w.forcedChunks))
	for p := range w.forcedChunks {
		out = append(out, p)
	}
	w.forcedChunksMu.RUnlock()
	sortChunkPositions(out)
	return out
}

// ForceChunk keeps a chunk loaded and included in simulation until unforced.
// It must be called on the world's transaction goroutine.
func (tx *Tx) ForceChunk(pos ChunkPos, force bool) error {
	w := tx.World()
	w.loadForcedChunks()
	w.forcedChunksMu.Lock()
	if force {
		if w.forcedChunks == nil {
			w.forcedChunks = map[ChunkPos]struct{}{}
		}
		w.forcedChunks[pos] = struct{}{}
	} else {
		delete(w.forcedChunks, pos)
	}
	w.forcedChunksMu.Unlock()
	if force {
		if c := tx.chunk(pos); c != nil {
			c.modified = true
		}
	}
	return w.saveForcedChunks()
}

// ClearForcedChunks removes all forced chunk tickets. It must run on the world owner.
func (tx *Tx) ClearForcedChunks() error {
	w := tx.World()
	w.forcedChunksMu.Lock()
	clear(w.forcedChunks)
	w.forcedChunksMu.Unlock()
	return w.saveForcedChunks()
}

// IsChunkForced reports whether a chunk has a persistent force-load ticket.
func (w *World) IsChunkForced(pos ChunkPos) bool {
	if w == nil {
		return false
	}
	w.loadForcedChunks()
	w.forcedChunksMu.RLock()
	_, ok := w.forcedChunks[pos]
	w.forcedChunksMu.RUnlock()
	return ok
}

func (w *World) forcedChunksNamespace() string {
	return forcedChunksNamespacePrefix + dimensionStorageKey(w.Dimension())
}

func (w *World) loadForcedChunks() {
	w.forcedChunksMu.Lock()
	defer w.forcedChunksMu.Unlock()
	if w.forcedChunksLoaded {
		return
	}
	w.forcedChunksLoaded = true
	w.forcedChunks = map[ChunkPos]struct{}{}
	data, err := w.LoadWorldData(w.forcedChunksNamespace())
	if errors.Is(err, leveldb.ErrNotFound) {
		return
	}
	if err != nil {
		w.conf.Log.Error("load forced chunks: " + err.Error())
		return
	}
	var positions []ChunkPos
	if err = json.Unmarshal(data, &positions); err != nil {
		w.conf.Log.Error("decode forced chunks: " + err.Error())
		return
	}
	for _, p := range positions {
		w.forcedChunks[p] = struct{}{}
	}
}

func (w *World) saveForcedChunks() error {
	w.forcedChunksMu.RLock()
	positions := make([]ChunkPos, 0, len(w.forcedChunks))
	for p := range w.forcedChunks {
		positions = append(positions, p)
	}
	w.forcedChunksMu.RUnlock()
	sortChunkPositions(positions)
	data, err := json.Marshal(positions)
	if err != nil {
		return err
	}
	return w.SaveWorldData(w.forcedChunksNamespace(), data)
}

func sortChunkPositions(positions []ChunkPos) {
	slices.SortFunc(positions, func(a, b ChunkPos) int {
		if a[0] < b[0] {
			return -1
		}
		if a[0] > b[0] {
			return 1
		}
		if a[1] < b[1] {
			return -1
		}
		if a[1] > b[1] {
			return 1
		}
		return 0
	})
}

// ChunkPosition returns the chunk containing a block position.
func ChunkPosition(pos cube.Pos) ChunkPos { return chunkPosFromBlockPos(pos) }

// ResendChunk sends the current chunk contents to all viewers of the chunk.
func (tx *Tx) ResendChunk(pos ChunkPos) {
	if c, ok := tx.World().loadedChunk(pos); ok {
		for viewer := range c.viewers {
			viewer.ViewChunk(pos, tx.World().Dimension(), c)
		}
	}
}
