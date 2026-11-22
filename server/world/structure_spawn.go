package world

import (
	"sync"

	"github.com/df-mc/dragonfly/internal/faultcheck"
	"github.com/df-mc/dragonfly/server/block/cube"
)

// StructureArea is the area a generated structure start covers: the bounding
// box of the whole start and those of its pieces, all inclusive.
type StructureArea struct {
	ID     string
	Box    [2]cube.Pos
	Pieces [][2]cube.Pos
}

// Contains reports whether pos lies in the bounding box of the start, or of
// one of its pieces if pieces is set.
func (a StructureArea) Contains(pos cube.Pos, pieces bool) bool {
	if !pieces {
		return boxContainsPos(a.Box, pos)
	}
	for _, p := range a.Pieces {
		if boxContainsPos(p, pos) {
			return true
		}
	}
	return false
}

func boxContainsPos(box [2]cube.Pos, pos cube.Pos) bool {
	return pos[0] >= box[0][0] && pos[0] <= box[1][0] && pos[1] >= box[0][1] && pos[1] <= box[1][1] && pos[2] >= box[0][2] && pos[2] <= box[1][2]
}

// SpawnStructureFinder is implemented by generators that place structures
// overriding the natural spawns within them: it returns those whose starts
// overlap a chunk, as the structure references Java keeps in each chunk.
type SpawnStructureFinder interface {
	SpawnStructures(pos ChunkPos) []StructureArea
}

const (
	// spawnStructureCacheMax bounds the cached spawn-structure chunk lookups.
	spawnStructureCacheMax = 16384
	// spawnStructureWorkers bounds concurrent background spawn-structure lookups
	// so cache-miss bursts never spawn unbounded goroutines.
	spawnStructureWorkers = 4
	// spawnStructureQueueMax bounds pending background spawn-structure lookups.
	spawnStructureQueueMax = 256
)

// spawnStructureCache keeps the spawn structures of the chunks asked for.
type spawnStructureCache struct {
	mu      sync.RWMutex
	chunks  map[ChunkPos][]StructureArea
	ring    []ChunkPos
	ringPos int
	pending map[ChunkPos]struct{}
	queue   []ChunkPos
	head    int
	workers int
}

// SpawnStructures returns the structures overriding natural spawns whose
// starts overlap the chunk at pos. In asynchronous worlds, cache misses queue a
// background lookup and return nil until ready so the world owner never stalls
// on structure generation.
func (w *World) SpawnStructures(pos ChunkPos) []StructureArea {
	if w == nil {
		return nil
	}
	finder, ok := w.conf.Generator.(SpawnStructureFinder)
	if !ok {
		return nil
	}
	c := &w.spawnStructures
	c.mu.RLock()
	if areas, hit := c.chunks[pos]; hit {
		c.mu.RUnlock()
		return areas
	}
	c.mu.RUnlock()
	if w.conf.Synchronous {
		areas := finder.SpawnStructures(pos)
		c.mu.Lock()
		c.storeLocked(pos, areas)
		c.mu.Unlock()
		return areas
	}
	return w.queueSpawnStructures(finder, pos)
}

func (w *World) queueSpawnStructures(finder SpawnStructureFinder, pos ChunkPos) []StructureArea {
	if w.closed.Load() {
		return nil
	}
	c := &w.spawnStructures
	c.mu.Lock()
	if areas, hit := c.chunks[pos]; hit {
		c.mu.Unlock()
		return areas
	}
	if _, inFlight := c.pending[pos]; inFlight || len(c.queue)-c.head >= spawnStructureQueueMax {
		c.mu.Unlock()
		return nil
	}
	if c.pending == nil {
		c.pending = make(map[ChunkPos]struct{})
	}
	c.pending[pos] = struct{}{}
	c.queue = append(c.queue, pos)
	needWorker := c.workers < spawnStructureWorkers
	if needWorker {
		c.workers++
	}
	c.mu.Unlock()
	if needWorker {
		w.startSpawnStructureWorker(finder)
	}
	return nil
}

func (w *World) startSpawnStructureWorker(finder SpawnStructureFinder) {
	w.scheduleMu.Lock()
	if w.closed.Load() {
		w.scheduleMu.Unlock()
		c := &w.spawnStructures
		c.mu.Lock()
		c.workers--
		c.mu.Unlock()
		return
	}
	w.generatorRunning.Add(1)
	w.scheduleMu.Unlock()
	go w.runSpawnStructureWorker(finder)
}

func (w *World) runSpawnStructureWorker(finder SpawnStructureFinder) {
	defer w.generatorRunning.Done()
	c := &w.spawnStructures
	for {
		c.mu.Lock()
		if w.closed.Load() || c.head >= len(c.queue) {
			if w.closed.Load() {
				clear(c.pending)
				c.queue, c.head = nil, 0
			}
			c.workers--
			c.mu.Unlock()
			return
		}
		pos := c.queue[c.head]
		c.queue[c.head] = ChunkPos{}
		c.head++
		if c.head == len(c.queue) {
			c.queue, c.head = c.queue[:0], 0
		} else if c.head >= 64 && c.head*2 >= len(c.queue) {
			n := copy(c.queue, c.queue[c.head:])
			c.queue, c.head = c.queue[:n], 0
		}
		c.mu.Unlock()

		areas, ok := w.computeSpawnStructures(finder, pos)
		c.mu.Lock()
		delete(c.pending, pos)
		if ok {
			c.storeLocked(pos, areas)
		}
		c.mu.Unlock()
	}
}

func (w *World) computeSpawnStructures(finder SpawnStructureFinder, pos ChunkPos) (areas []StructureArea, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			faultcheck.Report("world spawn structures", r)
			areas, ok = nil, false
		}
	}()
	return finder.SpawnStructures(pos), true
}

func (c *spawnStructureCache) storeLocked(pos ChunkPos, areas []StructureArea) {
	if c.chunks == nil {
		c.chunks = make(map[ChunkPos][]StructureArea)
	}
	if _, exists := c.chunks[pos]; exists {
		c.chunks[pos] = areas
		return
	}
	if len(c.chunks) >= spawnStructureCacheMax {
		old := c.ring[c.ringPos]
		delete(c.chunks, old)
		c.ring[c.ringPos] = pos
		c.ringPos++
		if c.ringPos == len(c.ring) {
			c.ringPos = 0
		}
	} else {
		c.ring = append(c.ring, pos)
	}
	c.chunks[pos] = areas
}
