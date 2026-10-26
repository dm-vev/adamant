package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Prefetch implements world.Prefetcher. In the parallel phase before the
// entities of a world tick, a mob reads what its tick is going to read from
// the world as it stands: the fluids around its box and the collision boxes
// of the blocks it may move through. The tick uses a result only while the
// box it was read for and the block versions of the columns it read still
// match, so it gets exactly what reading the world itself would give.
func (e *Ent) Prefetch(self world.Entity, v *world.ReadView) {
	if ai, _ := e.mobAI(); ai != nil {
		ai.prefetch(e, self, v)
	}
}

var _ world.Prefetcher = (*Ent)(nil)

// verifyPrefetch makes ticks that use a prefetched result read the world
// as well and panic if the two differ, counting the checks in
// verifiedFluids and verifiedCollisions. Tests set it.
var (
	verifyPrefetch                     bool
	verifiedFluids, verifiedCollisions int
)

// prefetchColumns is the most columns a prefetched result depends on.
const prefetchColumns = 9

// columnVersions are the block versions of the columns a prefetched result
// was read from.
type columnVersions struct {
	n   int
	pos [prefetchColumns]world.ChunkPos
	ver [prefetchColumns]uint64
}

// record notes the versions of the columns under the blocks from lo to hi,
// inclusive. It fails if one is not readable or there are too many.
func (c *columnVersions) record(v *world.ReadView, lo, hi cube.Pos) bool {
	c.n = 0
	for cx := int32(lo[0]) >> 4; cx <= int32(hi[0])>>4; cx++ {
		for cz := int32(lo[2]) >> 4; cz <= int32(hi[2])>>4; cz++ {
			if c.n == prefetchColumns {
				return false
			}
			ver, ok := v.ColumnBlockVersion(world.ChunkPos{cx, cz})
			if !ok {
				return false
			}
			c.pos[c.n], c.ver[c.n] = world.ChunkPos{cx, cz}, ver
			c.n++
		}
	}
	return true
}

// current reports whether none of the columns changed since record.
func (c *columnVersions) current(tx *world.Tx) bool {
	for i := range c.n {
		if ver, ok := tx.ColumnBlockVersion(c.pos[i]); !ok || ver != c.ver[i] {
			return false
		}
	}
	return true
}

// mobPrefetch holds what the parallel phase read for a mob.
type mobPrefetch struct {
	fluidOK   bool
	fluidBox  cube.BBox
	fluid     fluidContact
	fluidCols columnVersions

	collisions collisionPrefetch
}

// collisionPrefetch holds the collision boxes of the blocks from lo up to hi,
// exclusive, in the order blockBBoxsAround visits them.
type collisionPrefetch struct {
	ok     bool
	lo, hi cube.Pos
	cells  []collisionCell
	boxes  []cube.BBox
	cols   columnVersions
}

// collisionCell is a block with collision boxes; its boxes, moved to its
// position, are boxes[from:to] of the prefetch.
type collisionCell struct {
	pos      cube.Pos
	from, to int32
}

// prefetch reads the fluids and collision boxes around the mob for its next
// tick.
func (ai *MobAI) prefetch(e *Ent, self world.Entity, v *world.ReadView) {
	p := &ai.pre
	p.fluidOK, p.collisions.ok = false, false
	box := self.H().Type().BBox(self).Translate(e.data.Pos)

	// updateFluids measures the fluids around the box the mob starts its
	// tick with. Measuring with currents as well serves either case.
	v.Reset()
	fluid := measureFluids(v, box, true)
	lo, hi := fluidReadRange(box)
	if !v.Missed() && p.fluidCols.record(v, lo, hi) {
		p.fluid, p.fluidBox, p.fluidOK = fluid, box, true
	}

	// collide reads the blocks around the box extended by the movement of
	// the tick and the step height. Read with room for the velocity the mob
	// has now and for stepping up a block.
	vel := e.data.Vel
	horizontal := .75 + math.Max(math.Abs(vel[0]), math.Abs(vel[2]))
	down := .75 + math.Abs(vel[1])
	up := math.Max(ai.StepHeight, 1) + .5 + math.Abs(vel[1])
	area := cube.Box(box.Min()[0]-horizontal, box.Min()[1]-down, box.Min()[2]-horizontal, box.Max()[0]+horizontal, box.Max()[1]+up, box.Max()[2]+horizontal)
	v.Reset()
	p.collisions.read(v, area)
}

// fluidReadRange returns the blocks measureFluids may read for box: those
// it measures and their neighbours, whose liquid it compares.
func fluidReadRange(box cube.BBox) (cube.Pos, cube.Pos) {
	lo, hi := box.Min(), box.Max()
	return cube.Pos{int(math.Floor(lo[0])) - 1, int(math.Floor(lo[1])) - 1, int(math.Floor(lo[2])) - 1},
		cube.Pos{int(math.Ceil(hi[0])) + 1, int(math.Ceil(hi[1])) + 1, int(math.Ceil(hi[2])) + 1}
}

// read reads the collision boxes of the blocks area spans, as
// blockBBoxsAround would for area.
func (c *collisionPrefetch) read(v *world.ReadView, area cube.BBox) {
	c.ok = false
	c.cells, c.boxes = c.cells[:0], c.boxes[:0]
	min, max := area.Min(), area.Max()
	c.lo = cube.Pos{int(math.Floor(min[0])), int(math.Floor(min[1])), int(math.Floor(min[2]))}
	c.hi = cube.Pos{int(math.Ceil(max[0])), int(math.Ceil(max[1])), int(math.Ceil(max[2]))}
	for y := c.lo[1]; y < c.hi[1]; y++ {
		for x := c.lo[0]; x < c.hi[0]; x++ {
			for z := c.lo[2]; z < c.hi[2]; z++ {
				pos := cube.Pos{x, y, z}
				boxes := v.Block(pos).Model().BBox(pos, v)
				if len(boxes) == 0 {
					continue
				}
				from := int32(len(c.boxes))
				offset := mgl64.Vec3{float64(x), float64(y), float64(z)}
				for _, bb := range boxes {
					c.boxes = append(c.boxes, bb.Translate(offset))
				}
				c.cells = append(c.cells, collisionCell{pos: pos, from: from, to: int32(len(c.boxes))})
			}
		}
	}
	// Models of a block may look at the blocks beside it.
	if !v.Missed() && c.cols.record(v, c.lo.Sub(cube.Pos{1, 1, 1}), c.hi) {
		c.ok = true
	}
}

// boxesAround returns what blockBBoxsAround(tx, box) returns, from the
// prefetched boxes, if they cover it and are current.
func (c *collisionPrefetch) boxesAround(tx *world.Tx, box cube.BBox) ([]cube.BBox, bool) {
	if !c.ok {
		return nil, false
	}
	grown := box.Grow(0.25)
	min, max := grown.Min(), grown.Max()
	lo := cube.Pos{int(math.Floor(min[0])), int(math.Floor(min[1])), int(math.Floor(min[2]))}
	hi := cube.Pos{int(math.Ceil(max[0])), int(math.Ceil(max[1])), int(math.Ceil(max[2]))}
	if lo[0] < c.lo[0] || lo[1] < c.lo[1] || lo[2] < c.lo[2] || hi[0] > c.hi[0] || hi[1] > c.hi[1] || hi[2] > c.hi[2] {
		return nil, false
	}
	if !c.cols.current(tx) {
		c.ok = false
		return nil, false
	}
	var out []cube.BBox
	for _, cell := range c.cells {
		p := cell.pos
		if p[0] < lo[0] || p[0] >= hi[0] || p[1] < lo[1] || p[1] >= hi[1] || p[2] < lo[2] || p[2] >= hi[2] {
			continue
		}
		if out == nil {
			predicted := (hi[0]-lo[0])*(hi[1]-lo[1])*(hi[2]-lo[2]) + 2
			out = blockBBoxPool.Get().([]cube.BBox)
			if cap(out) < predicted {
				blockBBoxPool.Put(out[:0])
				out = make([]cube.BBox, 0, predicted)
			} else {
				out = out[:0]
			}
		}
		out = append(out, c.boxes[cell.from:cell.to]...)
	}
	return out, true
}

// prefetchedFluids returns the fluids prefetched for box if they are
// current.
func (p *mobPrefetch) prefetchedFluids(tx *world.Tx, box cube.BBox) (fluidContact, bool) {
	if !p.fluidOK || p.fluidBox != box {
		return fluidContact{}, false
	}
	p.fluidOK = false
	if !p.fluidCols.current(tx) {
		return fluidContact{}, false
	}
	return p.fluid, true
}
