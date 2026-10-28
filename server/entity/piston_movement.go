package entity

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/go-gl/mathgl/mgl32"
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
