package world

import (
	"sync"

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

// spawnStructureCache keeps the spawn structures of the chunks asked for.
type spawnStructureCache struct {
	mu     sync.Mutex
	chunks map[ChunkPos][]StructureArea
}

// SpawnStructures returns the structures overriding natural spawns whose
// starts overlap the chunk at pos.
func (w *World) SpawnStructures(pos ChunkPos) []StructureArea {
	finder, ok := w.conf.Generator.(SpawnStructureFinder)
	if !ok {
		return nil
	}
	c := &w.spawnStructures
	c.mu.Lock()
	if areas, ok := c.chunks[pos]; ok {
		c.mu.Unlock()
		return areas
	}
	c.mu.Unlock()
	areas := finder.SpawnStructures(pos)
	c.mu.Lock()
	if c.chunks == nil || len(c.chunks) >= 16384 {
		c.chunks = make(map[ChunkPos][]StructureArea)
	}
	c.chunks[pos] = areas
	c.mu.Unlock()
	return areas
}
