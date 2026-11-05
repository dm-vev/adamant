package anticheat

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/go-gl/mathgl/mgl64"
)

// RayHitsBox reports whether the ray from origin along dir passes through
// box, grown by margin on every side, within length blocks of origin. dir
// need not be a unit vector, but must not be zero.
func RayHitsBox(origin, dir mgl64.Vec3, box cube.BBox, margin, length float64) bool {
	n := dir.Len()
	if n == 0 || math.IsNaN(n) || math.IsInf(n, 0) {
		return false
	}
	dir = dir.Mul(1 / n)
	low, high := box.Min().Sub(mgl64.Vec3{margin, margin, margin}), box.Max().Add(mgl64.Vec3{margin, margin, margin})
	// The slab method: the span of the ray inside each pair of planes, and
	// the ray is in the box if the three spans meet.
	tMin, tMax := 0.0, length
	for i := range 3 {
		if math.Abs(dir[i]) < 1e-12 {
			if origin[i] < low[i] || origin[i] > high[i] {
				return false
			}
			continue
		}
		a, b := (low[i]-origin[i])/dir[i], (high[i]-origin[i])/dir[i]
		if a > b {
			a, b = b, a
		}
		tMin, tMax = max(tMin, a), min(tMax, b)
		if tMin > tMax {
			return false
		}
	}
	return true
}
