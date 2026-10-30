package world

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/go-gl/mathgl/mgl64"
)

func vibrationOccluded(tx *Tx, origin, destination mgl64.Vec3) bool {
	from := vibrationCellPosition(origin).Vec3Centre()
	to := vibrationCellPosition(destination).Vec3Centre()
	for axis := range 3 {
		from[axis], to[axis] = float64(float32(from[axis])), float64(float32(to[axis]))
	}
	const epsilon = float64(float32(.001))
	for _, offset := range [...]mgl64.Vec3{{0, -epsilon, 0}, {0, epsilon, 0}, {0, 0, -epsilon}, {0, 0, epsilon}, {-epsilon, 0, 0}, {epsilon, 0, 0}} {
		start := from
		for axis := range 3 {
			start[axis] = float64(float32(from[axis]) + float32(offset[axis]))
		}
		if !vibrationRayOccluded(tx.World(), start, to) {
			return false
		}
	}
	return true
}

// Vibration clips select only wool, whose native outline is a full unit cube.
// Merely visiting a cell is insufficient: a segment entirely inside that cube
// crosses none of its faces and the native ray/AABB test returns no hit.
func vibrationRayOccluded(w *World, from, to mgl64.Vec3) bool {
	return walkVibrationRay(from, to, func(pos cube.Pos) bool {
		block, loaded := w.blockLoaded(pos)
		return loaded && w.gameEventBlockFlags(block)&vibrationOccluding != 0 && vibrationRayHitsCube(pos, from, to)
	})
}

// walkVibrationRay follows native BlockSource::clip: the current point and
// remaining boundary fractions are recalculated in binary32 after every step.
// Ties choose Z, then Y, then X. There is no endpoint expansion. The native
// 200-step bound also terminates precision-induced cycles at large coordinates.
func walkVibrationRay(from, to mgl64.Vec3, blocked func(cube.Pos) bool) bool {
	var current, target [3]float32
	for axis := range 3 {
		current[axis], target[axis] = float32(from[axis]), float32(to[axis])
		if math.IsNaN(float64(current[axis])) || math.IsNaN(float64(target[axis])) {
			return false
		}
	}
	pos, end := vibrationCellPosition(from), vibrationCellPosition(to)
	if blocked(pos) {
		return true
	}
	for range 200 {
		if pos == end {
			return false
		}
		var boundary, delta [3]float32
		fraction := [3]float32{999, 999, 999}
		for axis := range 3 {
			if math.IsNaN(float64(current[axis])) {
				return false
			}
			delta[axis] = target[axis] - current[axis]
			if pos[axis] != end[axis] {
				boundary[axis] = float32(pos[axis])
				if end[axis] > pos[axis] {
					boundary[axis] += 1
				}
				fraction[axis] = (boundary[axis] - current[axis]) / delta[axis]
			}
		}
		axis := 2
		if fraction[0] < fraction[1] && fraction[0] < fraction[2] {
			axis = 0
		} else if fraction[1] < fraction[2] {
			axis = 1
		}
		for other := range 3 {
			if other == axis {
				current[other] = boundary[axis]
			} else {
				current[other] += float32(delta[other] * fraction[axis])
			}
		}
		if end[axis] > pos[axis] {
			pos[axis]++
		} else {
			pos[axis]--
		}
		if blocked(pos) {
			return true
		}
	}
	return false
}

// vibrationRayHitsCube ports the boolean result of the native ray/AABB
// constructor, including its squared parallel threshold and inclusive faces.
func vibrationRayHitsCube(pos cube.Pos, from, to mgl64.Vec3) bool {
	var start, delta, extent [3]float32
	for axis := range 3 {
		base := float32(pos[axis])
		extent[axis] = float32(base+1) - base
		if extent[axis] <= 0 {
			return false
		}
		start[axis] = float32(from[axis]) - base
		delta[axis] = (float32(to[axis]) - base) - start[axis]
	}
	for axis := range 3 {
		if delta[axis]*delta[axis] < float32(1e-7) {
			continue
		}
		for _, face := range [2]float32{0, extent[axis]} {
			t := (face - start[axis]) / delta[axis]
			if !(t >= 0 && t <= 1) {
				continue
			}
			inside := true
			for other := range 3 {
				if other != axis {
					p := start[other] + float32(delta[other]*t)
					inside = inside && p >= 0 && p <= extent[other]
				}
			}
			if inside {
				return true
			}
		}
	}
	return false
}
