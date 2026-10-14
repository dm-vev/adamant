package world

import "math"

// SunAngle returns Java 26.3's visual/sun_angle environment attribute in
// degrees. The Overworld day timeline eases the sun from 0 degrees at noon
// round to 360 degrees at the next noon; other dimensions keep the
// attribute's default of 0.
func (tx *Tx) SunAngle() float32 {
	if tx.World().Dimension() != Overworld {
		return 0
	}
	return javaSunAngle(tx.World().Time())
}

// javaSunAngle samples the SUN_ANGLE track of Timelines.OVERWORLD_DAY: its
// keyframes 360 and 0 both sit at tick 6000, so over each 24000 tick period
// the angle rises from 0 to 360 along the sky angle easing.
func javaSunAngle(tick int) float32 {
	tick = ((tick-6000)%TimeFull + TimeFull) % TimeFull
	alpha := float32(tick) / float32(TimeFull)
	return float32(360) * skyAngleEase.apply(alpha)
}

// skyAngleEase is Timelines' EasingType.symmetricCubicBezier(0.362, 0.241).
var skyAngleEase = newCubicBezierEase(0.362, 0.241, 1-0.362, 1-0.241)

// cubicBezierEase ports EasingType.CubicBezier.
type cubicBezierEase struct {
	x, y cubicCurve
}

func newCubicBezierEase(x1, y1, x2, y2 float32) cubicBezierEase {
	return cubicBezierEase{x: curveFromControls(x1, x2), y: curveFromControls(y1, y2)}
}

func curveFromControls(v1, v2 float32) cubicCurve {
	return cubicCurve{
		a: float32(float32(3*v1)-float32(3*v2)) + 1,
		b: float32(-6*v1) + float32(3*v2),
		c: 3 * v1,
	}
}

func (e cubicBezierEase) apply(x float32) float32 {
	return e.y.sample(e.solveT(x))
}

// solveT finds the curve parameter of x by Newton-Raphson, falling back to
// bisection.
func (e cubicBezierEase) solveT(x float32) float32 {
	t := x
	for range 4 {
		err := e.x.sample(t) - x
		if float32(math.Abs(float64(err))) < 1e-5 {
			return t
		}
		gradient := e.x.gradient(t)
		if gradient < 1e-5 {
			break
		}
		t -= min(max(err/gradient, -0.25), 0.25)
	}
	return e.solveTBisect(x, t)
}

func (e cubicBezierEase) solveTBisect(x, t float32) float32 {
	t0, t1 := float32(0), float32(1)
	for t0 < t1 {
		err := e.x.sample(t) - x
		if float32(math.Abs(float64(err))) < 1e-5 {
			return t
		}
		if err < 0 {
			t0 = t
		} else {
			t1 = t
		}
		t = (t1 + t0) / 2
	}
	return t
}

// cubicCurve is EasingType.CubicBezier.CubicCurve: a*t^3 + b*t^2 + c*t.
type cubicCurve struct {
	a, b, c float32
}

func (c cubicCurve) sample(t float32) float32 {
	return float32(float32(float32(float32(c.a*t)+c.b)*t)+c.c) * t
}

func (c cubicCurve) gradient(t float32) float32 {
	return float32(float32(float32(float32(3*c.a)*t)+float32(2*c.b))*t) + c.c
}
