package world

import (
	"fmt"
	"slices"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/go-gl/mathgl/mgl64"
)

// ReadView reads the blocks of a world from any goroutine while the world's
// owner waits for a parallel phase it started, during which nothing writes
// to the world. It only reads columns that are loaded and settled: it never
// loads, generates or completes a column, and keeps its own cache, so views
// on different goroutines share no mutable state. A read it cannot answer
// that way returns air and marks the view missed, telling the caller to do
// the work on the owner instead.
//
// A ReadView must only be used within the parallel phase of the world it was
// made for.
type ReadView struct {
	w       *World
	lastPos ChunkPos
	last    *Column
	missed  bool
	// tracking makes the view list the columns it reads in tracked.
	tracking bool
	tracked  []ChunkPos
}

// NewReadView returns a view of the world of tx for a parallel phase tx's
// owner runs. Each goroutine must use its own view.
func (tx *Tx) NewReadView() *ReadView {
	return &ReadView{w: tx.World()}
}

// Reset clears the missed state of the view and stops tracking, as before
// reading for another caller.
func (v *ReadView) Reset() {
	v.missed, v.tracking, v.tracked = false, false, v.tracked[:0]
}

// Track makes the view list the columns it reads from now on, as Tracked
// returns them, so that a result read through it can be checked against
// their block versions later.
func (v *ReadView) Track() {
	v.tracking, v.tracked = true, v.tracked[:0]
	if v.last != nil {
		v.tracked = append(v.tracked, v.lastPos)
	}
}

// Tracked returns the columns read since Track. The slice is reused by the
// view.
func (v *ReadView) Tracked() []ChunkPos {
	return v.tracked
}

// World returns the world the view reads, for its settings: its blocks and
// entities must be read through the view.
func (v *ReadView) World() *World {
	return v.w
}

// EntityVelocity returns the velocity of the entity of h if it is in the
// world of the view.
func (v *ReadView) EntityVelocity(h *EntityHandle) (mgl64.Vec3, bool) {
	if h == nil || h.state == nil || h.w != v.w {
		return mgl64.Vec3{}, false
	}
	return h.data.Vel, true
}

// EntityPosition returns the position of the entity of h if it is in the
// world of the view.
func (v *ReadView) EntityPosition(h *EntityHandle) (mgl64.Vec3, bool) {
	if h == nil || h.state == nil || h.w != v.w {
		return mgl64.Vec3{}, false
	}
	return h.data.Pos, true
}

// Missed reports whether a read since the last Reset needed a column the
// view could not read.
func (v *ReadView) Missed() bool {
	return v.missed
}

// column returns the settled column at pos, or nil if there is none.
func (v *ReadView) column(pos ChunkPos) *Column {
	if v.last != nil && v.lastPos == pos {
		return v.last
	}
	c, ok := v.w.chunks[pos]
	if !ok || !c.settled {
		v.missed = true
		return nil
	}
	v.lastPos, v.last = pos, c
	if v.tracking && !slices.Contains(v.tracked, pos) {
		v.tracked = append(v.tracked, pos)
	}
	return c
}

// Range returns the vertical range of the world.
func (v *ReadView) Range() cube.Range {
	return v.w.ra
}

// Block returns the block at pos, as Tx.Block does for a settled column.
func (v *ReadView) Block(pos cube.Pos) Block {
	if pos.OutOfBounds(v.w.ra) {
		return v.w.conf.Blocks.Air()
	}
	c := v.column(chunkPosFromBlockPos(pos))
	if c == nil {
		return v.w.conf.Blocks.Air()
	}
	rid := c.Block(uint8(pos[0]), int16(pos[1]), uint8(pos[2]), 0)
	if v.w.conf.Blocks.NBTBlock(rid) {
		if b, ok := c.BlockEntities[pos]; ok {
			return b
		}
		// Tx.Block would store default data for the block first.
		v.missed = true
	}
	return v.w.conf.Blocks.BlockByRuntimeIDOrAir(rid)
}

// BlockLoaded returns the block at pos, as Tx.BlockLoaded does for a settled
// column. A column that is not loaded misses rather than reading as air, as
// a result read from it would not notice the column loading later.
func (v *ReadView) BlockLoaded(pos cube.Pos) (Block, bool) {
	if pos.OutOfBounds(v.w.ra) {
		return v.w.conf.Blocks.Air(), false
	}
	if v.column(chunkPosFromBlockPos(pos)) == nil {
		return v.w.conf.Blocks.Air(), false
	}
	return v.Block(pos), true
}

// BlockStateID returns the canonical source state of the block at pos, as
// Tx.BlockStateID does for a settled column.
func (v *ReadView) BlockStateID(pos cube.Pos) (uint32, error) {
	var c *Column
	if !pos.OutOfBounds(v.w.ra) {
		if c = v.column(chunkPosFromBlockPos(pos)); c == nil {
			return 0, fmt.Errorf("column of %v is not readable", pos)
		}
	}
	id := v.w.javaStateIDInColumn(c, pos)
	if _, ok := gamedata.BlockStateName(id); !ok {
		return 0, fmt.Errorf("Java block state at %v has no canonical source identity", pos)
	}
	return id, nil
}

// Liquid returns the liquid at pos in any layer, as Tx.Liquid does for a
// settled column.
func (v *ReadView) Liquid(pos cube.Pos) (Liquid, bool) {
	if pos.OutOfBounds(v.w.ra) {
		return nil, false
	}
	c := v.column(chunkPosFromBlockPos(pos))
	if c == nil {
		return nil, false
	}
	return v.w.liquidInColumn(c, pos)
}

// MayContainLiquid reports whether a block from lo to hi, inclusive, may
// hold a liquid, as Tx.MayContainLiquid does for settled columns.
func (v *ReadView) MayContainLiquid(lo, hi cube.Pos) bool {
	minY, maxY := max(lo[1], v.w.ra[0]), min(hi[1], v.w.ra[1])
	if minY > maxY {
		return false
	}
	low, high := chunkPosFromBlockPos(lo), chunkPosFromBlockPos(hi)
	for cx := low[0]; cx <= high[0]; cx++ {
		for cz := low[1]; cz <= high[1]; cz++ {
			c := v.column(ChunkPos{cx, cz})
			if c == nil {
				// Not knowing, the caller reads every block and misses.
				return true
			}
			if v.w.subChunksMayContainLiquid(c, minY, maxY) {
				return true
			}
		}
	}
	return false
}

// ColumnBlockVersion returns the block version of the settled column at pos.
func (v *ReadView) ColumnBlockVersion(pos ChunkPos) (uint64, bool) {
	c := v.column(pos)
	if c == nil {
		return 0, false
	}
	return c.blockVersion, true
}

// Prefetcher is implemented by entities that read part of what their next
// tick reads in the parallel phase before the entities of a world tick.
// Prefetch runs on any goroutine with the entity the world keeps for the
// handle and a view of its own. It must only read: the entity itself, other
// entities and the world through v, never through a transaction.
type Prefetcher interface {
	Prefetch(self Entity, v *ReadView)
}
