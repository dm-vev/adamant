package entity

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl32"
	"github.com/go-gl/mathgl/mgl64"
	"math"
)

// pistonClipResult follows the client ClipCollideResult. The penetration
// threshold is selected by the caller, separately from shape clipping.
type pistonClipResult struct {
	penetration            float32
	clipped, depenetrating mgl32.Vec3
	axis                   int
}

func pistonClipCollide(obstacle, body cube.BBox32, velocity mgl32.Vec3) pistonClipResult {
	result := pistonClipResult{clipped: velocity, depenetrating: velocity}
	low, high := obstacle.Min(), obstacle.Max()
	for axis := range 3 {
		if low[axis] >= high[axis] {
			return result
		}
	}
	var depth, sign mgl32.Vec3
	separated, separationAxis, gap := 0, 0, float32(0)
	for axis := range 3 {
		right := pistonSnapContact(body.Max()[axis] - low[axis])
		left := pistonSnapContact(high[axis] - body.Min()[axis])
		if right <= 0 || left <= 0 {
			separated++
			if separated > 1 {
				return result
			}
			separationAxis = axis
			if right <= 0 {
				gap, sign[axis] = right, -1
			} else {
				gap, sign[axis] = left, 1
			}
			continue
		}
		depth[axis] = min(right, left)
		sign[axis] = 1
		if left > right {
			sign[axis] = -1
		}
	}
	if separated != 0 {
		if gap-velocity[separationAxis]*sign[separationAxis] > 0 {
			result.depenetrating[separationAxis] = gap * sign[separationAxis]
			result.clipped[separationAxis] = result.depenetrating[separationAxis]
		}
		return result
	}
	axis := 0
	if depth[0] > depth[1] {
		axis = 1
	}
	if depth[axis] > depth[2] {
		axis = 2
	}
	result.penetration = depth[axis]
	result.axis = axis
	push := depth[axis] * sign[axis]
	if push > 0 {
		result.depenetrating[axis] = max(push, velocity[axis])
	} else {
		result.depenetrating[axis] = min(push, velocity[axis])
	}
	return result
}

func pistonSnapContact(value float32) float32 {
	if value >= -float32(.000001) && value <= float32(.000001) {
		return 0
	}
	return value
}

type pistonSweepResult struct {
	move        mgl32.Vec3
	penetration float32
	shape       cube.BBox32
}

func pistonSweep(moves []mgl32.Vec3, body cube.BBox32, obstacles []cube.BBox32, limits mgl32.Vec3) pistonSweepResult {
	result := pistonSweepResult{shape: body}
	for _, move := range moves {
		for n := len(obstacles) - 1; n >= 0; n-- {
			clip := pistonClipCollide(obstacles[n], result.shape, move)
			if limits[clip.axis] < clip.penetration {
				move = clip.clipped
			} else {
				move = clip.depenetrating
			}
			result.penetration += clip.penetration
		}
		result.shape = result.shape.Translate(move)
		result.move = result.move.Add(move)
	}
	return result
}

// nativeDepenetrationLimits follows ConfigureDepenetration (0x9027730).
// Component minima and temporary overrides are applied after the policy.
func nativeDepenetrationLimits(bits uint8, oneWayBlocks bool, minimum mgl32.Vec3, override *mgl32.Vec3) mgl32.Vec3 {
	limits := mgl32.Vec3{1, 1, 1}
	if bits&8 == 0 && (oneWayBlocks || bits&0x15 != 0) {
		limits = mgl32.Vec3{}
	}
	for axis := range 3 {
		limits[axis] = max(limits[axis], minimum[axis])
		if override != nil {
			limits[axis] = max(limits[axis], override[axis])
		}
	}
	return limits
}

func (c *MovementComputer) checkNativeCollision(tx *world.Tx, e world.Entity, pos, vel mgl64.Vec3) (mgl64.Vec3, mgl64.Vec3) {
	body := e.H().Type().BBox(e).Translate(pos)
	query := body.Extend(vel)
	lo, hi := query.Min(), query.Max()
	// MoveCollisionSystem includes support below the requested sweep and a
	// small overhead margin even for a purely horizontal move. Excluding a
	// touching floor lets depenetration incorrectly resolve through it.
	lo[1] = min(lo[1], float64(float32(body.Min()[1])+(-float32(.2)-float32(math.Abs(float64(float32(vel[1])))))))
	hi[1] = max(hi[1], float64(float32(body.Max()[1])+float32(.08)))
	query = cube.Box(lo[0], lo[1], lo[2], hi[0], hi[1], hi[2])
	to32 := func(b cube.BBox) cube.BBox32 {
		lo, hi := b.Min(), b.Max()
		return cube.Box32(float32(lo[0]), float32(lo[1]), float32(lo[2]), float32(hi[0]), float32(hi[1]), float32(hi[2]))
	}
	var boxes []cube.BBox
	if !c.NoBlockCollisions {
		source := c.blockSource
		if source == nil {
			source = tx
		}
		boxes = nativeBlockBBoxsAround(source, query, tx.Range())
	}
	obstacles := make([]cube.BBox32, 0, len(boxes))
	for _, box := range boxes {
		if box.IntersectsWith(query) {
			obstacles = append(obstacles, to32(box))
		}
	}
	if boxes != nil {
		blockBBoxPool.Put(boxes[:0])
	}
	requested := mgl32.Vec3{float32(vel[0]), float32(vel[1]), float32(vel[2])}
	result := pistonSweep([]mgl32.Vec3{{0, requested[1], 0}, {requested[0], 0, 0}, {0, 0, requested[2]}},
		to32(body), obstacles, nativeDepenetrationLimits(c.depenetrationBits, false, mgl32.Vec3{}, nil))
	// UpdateDepenetration remembers consecutive penetrating moves. Items do
	// not use PushTowardsClosestSpace; item escape owns bit 3 independently.
	if result.penetration >= .000001 {
		if c.depenetrationBits&2 != 0 {
			c.depenetrationBits |= 4
		}
		c.depenetrationBits |= 2
	} else {
		c.depenetrationBits &= 0x19
	}
	delta := vel
	var next mgl64.Vec3
	for axis := range 3 {
		centre := (result.shape.Min()[axis] + result.shape.Max()[axis]) * float32(.5)
		next[axis] = float64(centre)
		if axis == 1 {
			if c.nativeMobMovement {
				next[axis] = float64(result.shape.Min()[axis])
			} else {
				offset := float32(.125)
				if entityType, ok := e.H().Type().(interface{ NetworkOffset() float64 }); ok {
					offset = float32(entityType.NetworkOffset())
				}
				next[axis] = float64(centre - offset)
			}
		}
		delta[axis] = next[axis] - pos[axis]
		if result.move[axis] != requested[axis] {
			vel[axis] = 0
		}
	}
	c.nativeBox, c.nativeBoxPosition, c.nativeBoxSet = result.shape, next, true
	c.collidedHorizontally = result.move[0] != requested[0] || result.move[2] != requested[2]
	c.collidedVertically = result.move[1] != requested[1]
	if requested[1] != 0 {
		c.onGround = c.collidedVertically && requested[1] < 0
	}
	return delta, vel
}

// nativeBlockBBoxsAround follows BlockSource's collision fetch cell order and
// binary32 bounds. Moving blocks may expose shapes one cell outside their
// stored position, so the ordinary quarter-block movement margin is too small.
func nativeBlockBBoxsAround(source world.BlockSource, query cube.BBox, bounds cube.Range) []cube.BBox {
	lo, hi := query.Min(), query.Max()
	var first, last cube.Pos
	for axis := range 3 {
		first[axis] = int(math.Floor(float64(float32(lo[axis]) - 1)))
		last[axis] = int(math.Floor(float64(float32(hi[axis]) + 1)))
	}
	first[1], last[1] = max(first[1], bounds[0]), min(last[1], bounds[1])
	var boxes []cube.BBox
	for x := first[0]; x <= last[0]; x++ {
		for z := first[2]; z <= last[2]; z++ {
			for y := first[1]; y <= last[1]; y++ {
				pos := cube.Pos{x, y, z}
				for _, shape := range source.Block(pos).Model().BBox(pos, source) {
					if boxes == nil {
						boxes = blockBBoxPool.Get().([]cube.BBox)[:0]
					}
					boxes = append(boxes, shape.Translate(pos.Vec3()))
				}
			}
		}
	}
	return boxes
}
