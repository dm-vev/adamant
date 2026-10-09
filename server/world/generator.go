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
	// ScheduledTicks contains zero-delay block or fluid updates for generated
	// positions. The World publishes them on its owner goroutine.
	ScheduledTicks []chunk.ScheduledTick
	// PostProcessing contains packed local positions by vertical section,
	// starting at the dimension's minimum section. Duplicates and insertion
	// order within each section are significant.
	PostProcessing [][]uint16

	scheduledTickIndices map[generatedTickIndex]struct{}
	scheduledTickCount   int
}

// MarkForPostProcessing records a source generation marker. The caller routes
// the position to its owning column; positions outside r are ignored.
func (result *GenerationResult) MarkForPostProcessing(pos cube.Pos, r cube.Range) {
	if pos.OutOfBounds(r) {
		return
	}
	if result.PostProcessing == nil {
		result.PostProcessing = make([][]uint16, (r[1]>>4)-(r[0]>>4)+1)
	}
	section := (pos[1] >> 4) - (r[0] >> 4)
	packed := uint16(pos[0]&15 | (pos[1]&15)<<4 | (pos[2]&15)<<8)
	result.PostProcessing[section] = append(result.PostProcessing[section], packed)
}

type generatedTickIndex struct {
	pos      cube.Pos
	kind     chunk.ScheduledTickKind
	typeName string
}

func generatedTickIndexOf(pos cube.Pos, kind chunk.ScheduledTickKind, typeName string) generatedTickIndex {
	return generatedTickIndex{pos: pos, kind: kind, typeName: typeName}
}

func (result *GenerationResult) rebuildScheduledTickIndex() {
	result.scheduledTickIndices = make(map[generatedTickIndex]struct{}, len(result.ScheduledTicks))
	for _, tick := range result.ScheduledTicks {
		result.scheduledTickIndices[generatedTickIndexOf(tick.Pos, tick.Kind, tick.Type)] = struct{}{}
	}
	result.scheduledTickCount = len(result.ScheduledTicks)
}

// HasScheduledTick reports whether a generated tick for the registry type and
// position is already present. Kind keeps block and fluid identities distinct.
func (result *GenerationResult) HasScheduledTick(pos cube.Pos, kind chunk.ScheduledTickKind, typeName string) bool {
	index := generatedTickIndexOf(pos, kind, typeName)
	if result.scheduledTickIndices != nil && result.scheduledTickCount == len(result.ScheduledTicks) {
		_, exists := result.scheduledTickIndices[index]
		return exists
	}
	for _, tick := range result.ScheduledTicks {
		if tick.Pos == pos && tick.Kind == kind && tick.Type == typeName {
			return true
		}
	}
	return false
}

// ScheduleTick appends a generated tick unless the same type is already
// scheduled at its position. The first schedule retains its priority and order.
func (result *GenerationResult) ScheduleTick(tick chunk.ScheduledTick) {
	switch tick.Kind {
	case chunk.BlockTick, chunk.FluidTick:
	default:
		panic("generated tick: invalid kind")
	}
	if tick.Type == "" {
		panic("generated tick: empty registry type")
	}
	if result.scheduledTickIndices == nil || result.scheduledTickCount != len(result.ScheduledTicks) {
		result.rebuildScheduledTickIndex()
	}
	index := generatedTickIndexOf(tick.Pos, tick.Kind, tick.Type)
	if _, exists := result.scheduledTickIndices[index]; exists {
		return
	}
	result.ScheduledTicks = append(result.ScheduledTicks, tick)
	result.scheduledTickIndices[index] = struct{}{}
	result.scheduledTickCount = len(result.ScheduledTicks)
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
