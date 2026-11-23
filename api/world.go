package api

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// TickPhase identifies when a registered world tick system executes within an
// active simulation tick.
type TickPhase = world.TickPhase

const (
	PhasePreEntities  = world.PhasePreEntities
	PhasePostEntities = world.PhasePostEntities
	PhasePostTick     = world.PhasePostTick
)

// AddSystem registers fn to execute during phase on each active tick of w,
// returning an idempotent removal callback.
func AddSystem(w *world.World, phase TickPhase, fn func(tx *world.Tx, tick int64)) func() {
	if w == nil || fn == nil {
		return func() {}
	}
	return w.AddSystem(phase, fn)
}

// FillRID fills the inclusive box [lo, hi] with the registered block runtime ID
// rid, returning the number of blocks changed.
func FillRID(tx *world.Tx, lo, hi cube.Pos, rid uint32) int {
	if tx == nil || tx.Closed() {
		return 0
	}
	lo, hi = normalizeBox(lo, hi)
	return tx.FillRID(lo, hi, rid)
}

func normalizeBox(lo, hi cube.Pos) (cube.Pos, cube.Pos) {
	if lo[0] > hi[0] {
		lo[0], hi[0] = hi[0], lo[0]
	}
	if lo[1] > hi[1] {
		lo[1], hi[1] = hi[1], lo[1]
	}
	if lo[2] > hi[2] {
		lo[2], hi[2] = hi[2], lo[2]
	}
	return lo, hi
}

// SetBlockRID sets the block at pos directly from a registered runtime ID rid.
func SetBlockRID(tx *world.Tx, pos cube.Pos, rid uint32, opts *world.SetOpts) {
	if tx == nil || tx.Closed() {
		return
	}
	tx.SetBlockRID(pos, rid, opts)
}

// BlockRuntimeID returns the runtime ID of b in world.DefaultBlockRegistry.
func BlockRuntimeID(b world.Block) uint32 {
	return world.DefaultBlockRegistry.BlockRuntimeID(b)
}

// BlockRuntimeIDIn returns the runtime ID of b in reg (defaulting to
// world.DefaultBlockRegistry when reg is nil).
func BlockRuntimeIDIn(reg world.BlockRegistry, b world.Block) uint32 {
	if reg == nil {
		reg = world.DefaultBlockRegistry
	}
	return reg.BlockRuntimeID(b)
}
