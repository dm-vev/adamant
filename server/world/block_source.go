package world

import "github.com/df-mc/dragonfly/server/block/cube"

// BlockSource represents a source for obtaining blocks.
type BlockSource interface {
	// Block returns the block at the given position in the block source.
	Block(cube.Pos) Block
}

// worldSource is a wrapper around a world transaction that implements BlockSource.
type worldSource struct{ tx *Tx }

func (w worldSource) Block(pos cube.Pos) Block { return w.tx.block(pos) }

// loadedWorldSource reads blocks without loading or generating chunks.
type loadedWorldSource struct {
	w        *World
	complete bool
	order    int
}

func (s *loadedWorldSource) Block(pos cube.Pos) Block {
	if pos.OutOfBounds(s.w.ra) {
		return s.w.conf.Blocks.Air()
	}
	if block, ok := s.w.blockLoaded(pos); ok {
		column := s.w.chunks[chunkPosFromBlockPos(pos)]
		if _, pending := column.missingBlockStates[pos]; pending {
			if dependency, ok := block.(MissingStateReconstructor); ok && dependency.ReconstructionOrder() < s.order {
				s.complete = false
			}
		}
		return block
	}
	s.complete = false
	return s.w.conf.Blocks.Air()
}
