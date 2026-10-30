package trace

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"math"
)

// Shape selects the block geometry used by a trace.
type Shape uint8

const (
	// CollisionShape uses the physical entity-collision geometry.
	CollisionShape Shape = iota
	// RayShape uses the block's ray geometry when it differs from physical collision.
	RayShape
)

type rayModel interface {
	RayBBox(cube.Pos, world.BlockSource) []cube.BBox
}

func shapeBoxes(shape Shape, m world.BlockModel, pos cube.Pos, src world.BlockSource) []cube.BBox {
	if shape == RayShape {
		if ray, ok := m.(rayModel); ok {
			return ray.RayBBox(pos, src)
		}
	}
	return m.BBox(pos, src)
}

// BlockResult is the result of a ray trace collision with a block's model.
type BlockResult struct {
	bb   cube.BBox
	pos  mgl64.Vec3
	face cube.Face

	blockPos cube.Pos
}

// BBox returns the BBox that was collided within the block's model.
func (r BlockResult) BBox() cube.BBox {
	return r.bb
}

// Position ...
func (r BlockResult) Position() mgl64.Vec3 {
	return r.pos
}

// Face returns the hit block face.
func (r BlockResult) Face() cube.Face {
	return r.face
}

// BlockPosition returns the block that was collided with.
func (r BlockResult) BlockPosition() cube.Pos {
	return r.blockPos
}

// BlockIntercept traces against the selected block shape and finds the nearest hit to start.
// BlockIntercept returns a BlockResult with the block collided with and with the colliding vector closest to the start position,
// if no colliding point was found, a zero BlockResult is returned and ok is false.
func BlockIntercept(pos cube.Pos, src world.BlockSource, b world.Block, start, end mgl64.Vec3, shape Shape) (result BlockResult, ok bool) {
	bbs := shapeBoxes(shape, b.Model(), pos, src)
	return blockBoxesIntercept(pos, bbs, start, end)
}

func blockBoxesIntercept(pos cube.Pos, bbs []cube.BBox, start, end mgl64.Vec3) (result BlockResult, ok bool) {
	if len(bbs) == 0 {
		return
	}

	var (
		hit  Result
		dist = math.MaxFloat64
	)

	for _, bb := range bbs {
		next, ok := BBoxIntercept(bb.Translate(pos.Vec3()), start, end)
		if !ok {
			continue
		}

		nextDist := next.Position().Sub(start).LenSqr()
		if nextDist < dist {
			hit = next
			dist = nextDist
		}
	}

	if hit == nil {
		return result, false
	}

	return BlockResult{bb: hit.BBox(), pos: hit.Position(), face: hit.Face(), blockPos: pos}, true
}

// BlockIntersects checks if the line segment from start to end intersects the block model of b at pos. Unlike
// BlockIntercept, it only reports whether an intersection exists and does not calculate the closest hit position, face,
// or bounding box.
func BlockIntersects(pos cube.Pos, src world.BlockSource, b world.Block, start, end mgl64.Vec3) bool {
	m := b.Model()
	switch m.(type) {
	case model.Empty:
		return false
	case model.Solid:
		return BBoxIntersects(cube.Box(0, 0, 0, 1, 1, 1).Translate(pos.Vec3()), start, end)
	}

	for _, bb := range m.BBox(pos, src) {
		if BBoxIntersects(bb.Translate(pos.Vec3()), start, end) {
			return true
		}
	}
	return false
}
