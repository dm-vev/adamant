package world

import "math"

func nativeRandomFloat32(word uint32) float32 {
	return float32(float64(word) * (1.0 / 4294967296.0))
}

// Core::Random keeps a separate cached binary32 Gaussian pair. Its rejection
// test and each arithmetic step use binary32, including the rounded logf.
type nativeGaussianFloatState struct {
	value  float32
	cached bool
}

func (g *nativeGaussianFloatState) next(word func() uint32) float32 {
	if g.cached {
		g.cached = false
		return g.value
	}
	for {
		x, y := nativeRandomFloat32(word()), nativeRandomFloat32(word())
		x, y = float32(x+x)-1, float32(y+y)-1
		radius := float32(float32(x*x) + float32(y*y))
		if radius >= 1 || radius == 0 {
			continue
		}
		factor := nativeGaussianLog32(radius)
		factor = float32(factor * -2)
		factor = float32(factor / radius)
		factor = float32(math.Sqrt(float64(factor)))
		g.value, g.cached = float32(y*factor), true
		return float32(x * factor)
	}
}
