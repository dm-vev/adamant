package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func (l Leaves) Removed(pos cube.Pos, tx *world.Tx, replacement world.Block) {
	before, _ := l.EncodeBlock()
	after, _ := replacement.EncodeBlock()
	if before != after {
		flagNearbyLeafDecay(pos, tx, 1)
	}
}

func (l Log) Removed(pos cube.Pos, tx *world.Tx, replacement world.Block) {
	before, _ := l.EncodeBlock()
	after, _ := replacement.EncodeBlock()
	if before != after {
		flagNearbyLeafDecay(pos, tx, 4)
	}
}

// DecayingBlocksUtil::runDecay scans a cube, including its diagonal corners,
// only when the expanded region's chunks are available. Its flags-2 writes
// retain persistent_bit and do not recursively notify surrounding leaves.
func flagNearbyLeafDecay(pos cube.Pos, tx *world.Tx, radius int) {
	margin := radius + 1
	for x := (pos[0] - margin) >> 4; x <= (pos[0]+margin)>>4; x++ {
		for z := (pos[2] - margin) >> 4; z <= (pos[2]+margin)>>4; z++ {
			if _, ready := tx.ChunkState(world.ChunkPos{int32(x), int32(z)}); !ready {
				return
			}
		}
	}
	for x := pos[0] - radius; x <= pos[0]+radius; x++ {
		for y := pos[1] - radius; y <= pos[1]+radius; y++ {
			for z := pos[2] - radius; z <= pos[2]+radius; z++ {
				p := cube.Pos{x, y, z}
				// The kernel calls Removed before writing the replacement.
				// The native source already exposes the replacement at this cell.
				if p == pos || p.OutOfBounds(tx.Range()) {
					continue
				}
				leaf, ok := tx.Block(p).(Leaves)
				if !ok || leaf.ShouldUpdate {
					continue
				}
				leaf.ShouldUpdate = true
				tx.SetBlock(p, leaf, leavesUpdateBitOpts)
			}
		}
	}
}
