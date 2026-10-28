package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl32"
	"github.com/go-gl/mathgl/mgl64"
)

type itemEscapeResult struct {
	velocity     mgl32.Vec3
	stuck        bool
	hasCollision bool
	escaped      bool
}

// itemEscape follows Actor::pushOutOfBlocks, omitting redundant status-flag writes.
// Item-escape and block-property flags are inputs: collision shape does not establish
// either flag. The caller supplies the body's world box and its feet position.
func itemEscape(point, velocity mgl32.Vec3, body, single cube.BBox32, shapes []cube.BBox32, pushesOutItems bool, neighbours uint8, blockProperties uint64, heightOffset float32) itemEscapeResult {
	result := itemEscapeResult{velocity: velocity, hasCollision: true}
	if !pushesOutItems || !body.IntersectsWith(single) {
		return result
	}
	result.stuck = neighbours&63 == 63
	result.hasCollision = !result.stuck || len(shapes) >= 2
	var fraction mgl32.Vec3
	for axis, coordinate := range point {
		fraction[axis] = coordinate - float32(math.Floor(float64(coordinate)))
	}
	distance := float32(math.MaxFloat32)
	face := cube.Face(6)
	for _, candidate := range []struct {
		face     cube.Face
		distance float32
	}{
		{cube.FaceWest, fraction[0]}, {cube.FaceEast, 1 - fraction[0]},
		{cube.FaceDown, fraction[1]}, {cube.FaceUp, 1 - fraction[1]},
		{cube.FaceNorth, fraction[2]}, {cube.FaceSouth, 1 - fraction[2]},
	} {
		if neighbours&(1<<candidate.face) == 0 && distance > candidate.distance {
			face, distance = candidate.face, candidate.distance
		}
	}
	if face == cube.Face(6) {
		if !result.stuck {
			return result
		}
		face = cube.FaceUp
	}
	magnitude := float32(.25)
	if blockProperties&1 != 0 {
		magnitude = .1
	}
	if face == cube.FaceUp && !result.stuck && len(shapes) != 0 {
		var height float32
		intersects := false
		for _, shape := range shapes {
			if body.IntersectsWith(shape) {
				intersects = true
				height = shape.Max()[1] - shape.Min()[1]
			}
		}
		if intersects {
			if len(shapes) >= 2 {
				result.stuck = true
			}
			cellY := float32(math.Floor(float64(point[1])))
			magnitude *= max(height+(cellY+heightOffset*2)-point[1], 0)
		} else if len(shapes) >= 2 {
			return result
		}
	}
	var motion mgl32.Vec3
	switch face {
	case cube.FaceDown:
		motion[1] = -magnitude
	case cube.FaceUp:
		motion[1] = magnitude
	case cube.FaceNorth:
		motion[2] = -magnitude
	case cube.FaceSouth:
		motion[2] = magnitude
	case cube.FaceWest:
		motion[0] = -magnitude
	case cube.FaceEast:
		motion[0] = magnitude
	}
	result.velocity = motion
	result.escaped = true
	return result
}

func (i *ItemBehaviour) pushOutOfBlocks(e *Ent, tx *world.Tx) {
	pos := e.Position()
	cell := cube.PosFromVec3(pos)
	current := tx.Block(cell)
	name, _ := current.EncodeBlock()
	pushes, properties := nativeItemEscapeFlags(name)
	i.passive.mc.NoBlockCollisions = false
	i.passive.mc.depenetrationBits &^= 8
	if !pushes {
		return
	}
	shapes := current.Model().BBox(cell, tx)
	if len(shapes) == 0 {
		return
	}
	box32 := func(box cube.BBox) cube.BBox32 {
		lo, hi := box.Min(), box.Max()
		return cube.Box32(float32(lo[0]), float32(lo[1]), float32(lo[2]), float32(hi[0]), float32(hi[1]), float32(hi[2]))
	}
	worldShapes := make([]cube.BBox32, len(shapes))
	var lo, hi mgl32.Vec3
	for n, shape := range shapes {
		shape := box32(shape.Translate(cell.Vec3()))
		worldShapes[n] = shape
		if n == 0 {
			lo, hi = shape.Min(), shape.Max()
		} else {
			for axis := range 3 {
				lo[axis] = min(lo[axis], shape.Min()[axis])
				hi[axis] = max(hi[axis], shape.Max()[axis])
			}
		}
	}
	var neighbours uint8
	for face := cube.FaceDown; face <= cube.FaceEast; face++ {
		name, _ := tx.Block(cell.Side(face)).EncodeBlock()
		if pushes, _ := nativeItemEscapeFlags(name); pushes {
			neighbours |= 1 << face
		}
	}
	vel := e.Velocity()
	result := itemEscape(mgl32.Vec3{float32(pos[0]), float32(pos[1]), float32(pos[2])},
		mgl32.Vec3{float32(vel[0]), float32(vel[1]), float32(vel[2])},
		box32(e.H().Type().BBox(e).Translate(pos)), cube.Box32(lo[0], lo[1], lo[2], hi[0], hi[1], hi[2]),
		worldShapes, pushes, neighbours, properties, .125)
	i.passive.mc.NoBlockCollisions = !result.hasCollision
	if result.stuck {
		i.passive.mc.depenetrationBits |= 8
	}
	if result.escaped {
		e.data.Vel = mgl64.Vec3{float64(result.velocity[0]), float64(result.velocity[1]), float64(result.velocity[2])}
	}
}
