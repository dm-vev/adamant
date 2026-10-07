package world

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// Generator handles the generating of newly created chunks. Worlds have one generator which is used to
// generate chunks when the provider of the world cannot find a chunk at a given chunk position.
type Generator interface {
	// GenerateChunk populates blocks and biomes on a generation worker and
	// returns additional column data. Ownership transfers to the World when
	// this method returns; generators must not retain mutable returned data
	// or perform world transactions from generation workers.
	GenerateChunk(pos ChunkPos, chunk *chunk.Chunk) GenerationResult
	// DefaultSpawn returns the default spawn position for worlds using this generator in the dimension passed.
	DefaultSpawn(dim Dimension) cube.Pos
}

// GenerationResult contains data that cannot be represented by block runtime
// IDs alone. Its zero value describes a column with no additional data.
type GenerationResult struct {
	// Entities are newly constructed, worldless handles. The World activates
	// them on its owner goroutine before publishing the column to consumers.
	Entities []*EntityHandle
	// BlockEntities overrides the default NBT of blocks placed in the chunk.
	// Positions are absolute world coordinates and must refer to NBT blocks.
	BlockEntities []chunk.BlockEntity
	// ScheduledBlocks contains initial updates for generated blocks. Tick is
	// a relative delay from owner-thread publication, not an absolute tick.
	ScheduledBlocks []chunk.ScheduledBlockUpdate
}

// NopGenerator is the default generator a world. It places no blocks in the world which results in a void
// world.
type NopGenerator struct{}

// GenerateChunk ...
func (NopGenerator) GenerateChunk(ChunkPos, *chunk.Chunk) GenerationResult {
	return GenerationResult{}
}

// DefaultSpawn ...
func (NopGenerator) DefaultSpawn(Dimension) cube.Pos { return cube.Pos{} }
