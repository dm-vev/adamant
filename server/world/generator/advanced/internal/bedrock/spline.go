package bedrock

// terrainSpline is an immutable knot sequence. Child knots select another
// coordinate spline; scalar knots hold a constant. Trees are built once below.
type terrainSpline struct {
	coordinate terrainCoordinate
	points     []terrainPoint
}

type terrainCoordinate uint8

const (
	continentalness terrainCoordinate = iota
	erosion
	weirdness
	ridges
)

type terrainPoint struct {
	x, value, derivative float32
	child                *terrainSpline
}

func scalar(x, value, derivative float32) terrainPoint {
	return terrainPoint{x: x, value: value, derivative: derivative}
}

func nested(x float32, child *terrainSpline) terrainPoint {
	return terrainPoint{x: x, child: child}
}

func spline(coordinate terrainCoordinate, points ...terrainPoint) *terrainSpline {
	return &terrainSpline{coordinate: coordinate, points: points}
}

func (s *terrainSpline) sample(c, e, w, r float32) float32 {
	input := c
	switch s.coordinate {
	case erosion:
		input = e
	case weirdness:
		input = w
	case ridges:
		input = r
	}
	points := s.points
	last := len(points) - 1
	start := 0
	for start < len(points) && input >= points[start].x {
		start++
	}
	start--
	if start < 0 {
		p := &points[0]
		value := p.value
		if p.child != nil {
			value = p.child.sample(c, e, w, r)
		}
		if p.derivative == 0 {
			return value
		}
		delta := float32(input - p.x)
		return float32(value + float32(p.derivative*delta))
	}
	if start == last {
		p := &points[last]
		value := p.value
		if p.child != nil {
			value = p.child.sample(c, e, w, r)
		}
		if p.derivative == 0 {
			return value
		}
		delta := float32(input - p.x)
		return float32(value + float32(p.derivative*delta))
	}
	p1, p2 := &points[start], &points[start+1]
	dx := float32(p2.x - p1.x)
	t := float32(float32(input-p1.x) / dx)
	y1, y2 := p1.value, p2.value
	if p1.child != nil {
		y1 = p1.child.sample(c, e, w, r)
	}
	if p2.child != nil {
		y2 = p2.child.sample(c, e, w, r)
	}
	yDiff := float32(y2 - y1)
	a := float32(float32(p1.derivative*dx) - yDiff)
	b := float32(float32(-float32(p2.derivative*dx)) + yDiff)
	linear := float32(float32(yDiff*t) + y1)
	tComplement := float32(1 - t)
	shape := float32(float32(a + float32(float32(b-a)*t)))
	correction := float32(float32(t*tComplement) * shape)
	return float32(linear + correction)
}

func peaksAndValleys(weirdness float32) float32 {
	abs := func(v float32) float32 {
		if v < 0 {
			return -v
		}
		return v
	}
	inner := float32(abs(weirdness) - float32(0.6666667))
	shifted := float32(abs(inner) - float32(0.33333334))
	return float32(-float32(shifted * float32(3)))
}

func buildRidgeJaggedness(peakFactor, highFactor float32) *terrainSpline {
	start := peaksAndValleys(0.4)
	end := peaksAndValleys(0.56666666)
	middle := float32(float32(start+end) / float32(2))
	weirdnessSpline := func(factor float32) *terrainSpline {
		return spline(weirdness,
			scalar(-0.01, float32(float32(0.63)*factor), 0),
			scalar(0.01, float32(float32(0.3)*factor), 0),
		)
	}
	middlePoint := scalar(middle, 0, 0)
	if highFactor > 0 {
		middlePoint = nested(middle, weirdnessSpline(highFactor))
	}
	peakPoint := scalar(1, 0, 0)
	if peakFactor > 0 {
		peakPoint = nested(1, weirdnessSpline(peakFactor))
	}
	return spline(ridges, scalar(start, 0, 0), middlePoint, peakPoint)
}

func buildErosionJaggedness(peak0, peak1, high0, high1 float32) *terrainSpline {
	at0 := buildRidgeJaggedness(peak0, high0)
	at1 := buildRidgeJaggedness(peak1, high1)
	return spline(erosion,
		nested(-1, at0),
		nested(-0.78, at1),
		nested(-0.5775, at1),
		scalar(-0.375, 0, 0),
	)
}

func buildJaggedness() *terrainSpline {
	return spline(continentalness,
		scalar(-0.11, 0, 0),
		nested(0.03, buildErosionJaggedness(1, 0.5, 0, 0)),
		nested(0.65, buildErosionJaggedness(1, 1, 1, 0)),
	)
}

func getErosionFactor(base float32, shattered bool) *terrainSpline {
	baseSpline := spline(weirdness, scalar(-0.2, 6.3, 0), scalar(0.2, base, 0))
	cross := func(left, right float32) *terrainSpline {
		return spline(weirdness, scalar(left, 6.3, 0), scalar(right, 2.67, 0))
	}
	rising := func() *terrainSpline {
		return spline(weirdness, scalar(-0.05, 2.67, 0), scalar(0.05, 6.3, 0))
	}
	points := []terrainPoint{
		nested(-0.6, baseSpline), nested(-0.5, cross(-0.05, 0.05)), nested(-0.35, baseSpline),
		nested(-0.25, baseSpline), nested(-0.1, rising()), nested(0.03, baseSpline),
	}
	if shattered {
		shatteredW := spline(weirdness, scalar(0, base, 0), scalar(0.1, 0.625, 0))
		shatteredR := spline(ridges, scalar(-0.9, base, 0), nested(-0.69, shatteredW))
		points = append(points, scalar(0.35, base, 0), nested(0.45, shatteredR), nested(0.55, shatteredR), scalar(0.62, base, 0))
	} else {
		peaks := spline(ridges, nested(-0.7, baseSpline), scalar(-0.15, 1.37, 0))
		extra := spline(ridges, nested(0.45, baseSpline), scalar(0.7, 1.56, 0))
		points = append(points, nested(0.05, extra), nested(0.4, extra), nested(0.45, peaks), nested(0.55, peaks), scalar(0.58, base, 0))
	}
	return spline(erosion, points...)
}

func buildFactor() *terrainSpline {
	return spline(continentalness,
		scalar(-0.19, 3.95, 0),
		nested(-0.15, getErosionFactor(6.25, true)),
		nested(-0.1, getErosionFactor(5.47, true)),
		nested(0.03, getErosionFactor(5.08, true)),
		nested(0.06, getErosionFactor(4.69, false)),
	)
}

func ridgeSpline(valley, low, mid, high, peaks, minValleySteepness float32) *terrainSpline {
	d1 := float32(float32(0.5 * float32(low-valley)))
	if d1 < minValleySteepness {
		d1 = minValleySteepness
	}
	d2 := float32(5 * float32(mid-low))
	d1AtLow := d1
	if d2 < d1AtLow {
		d1AtLow = d2
	}
	return spline(ridges,
		scalar(-1, valley, d1), scalar(-0.4, low, d1AtLow), scalar(0, mid, d2),
		scalar(0.4, high, float32(2*float32(high-mid))), scalar(1, peaks, float32(0.7*float32(peaks-high))),
	)
}

func calculateSlope(y1, y2, x1, x2 float32) float32 {
	return float32(float32(y2-y1) / float32(x2-x1))
}

func mountainContinentalness(ridge, modulation float32) float32 {
	ridgeSlope := float32(1 - float32(float32(1-modulation)*float32(0.5)))
	ridgeIntersect := float32(float32(0.5) * float32(1-modulation))
	adjustedHeight := float32(float32(ridge+float32(1.17)) * float32(0.46082947))
	continental := float32(float32(adjustedHeight*ridgeSlope) - ridgeIntersect)
	if ridge < -0.7 {
		if continental < -0.2222 {
			return -0.2222
		}
	} else if continental < 0 {
		return 0
	}
	return continental
}

func mountainRidgeSpline(modulation float32, saddle bool) *terrainSpline {
	minValue := mountainContinentalness(-1, modulation)
	maxValue := mountainContinentalness(1, modulation)
	ridgeSlope := float32(1 - float32(float32(1-modulation)*float32(0.5)))
	ridgeIntersect := float32(float32(0.5) * float32(1-modulation))
	ridgeZero := float32(float32(ridgeIntersect/float32(float32(0.46082947)*ridgeSlope)) - float32(1.17))
	if ridgeZero > -0.65 && ridgeZero < 1 {
		before := mountainContinentalness(-0.75, modulation)
		after := mountainContinentalness(-0.65, modulation)
		minDerivative := calculateSlope(minValue, before, -1, -0.75)
		zeroValue := mountainContinentalness(ridgeZero, modulation)
		maxDerivative := calculateSlope(zeroValue, maxValue, ridgeZero, 1)
		return spline(ridges,
			scalar(-1, minValue, minDerivative), scalar(-0.75, before, 0), scalar(-0.65, after, 0),
			scalar(float32(ridgeZero-0.01), zeroValue, 0), scalar(ridgeZero, zeroValue, maxDerivative), scalar(1, maxValue, maxDerivative),
		)
	}
	derivative := calculateSlope(minValue, maxValue, -1, 1)
	if saddle {
		middle := float32(float32(float32(maxValue-minValue)*float32(0.5)) + minValue)
		if minValue < 0.2 {
			minValue = 0.2
		}
		return spline(ridges, scalar(-1, minValue, 0), scalar(0, middle, derivative), scalar(1, maxValue, derivative))
	}
	return spline(ridges, scalar(-1, minValue, derivative), scalar(1, maxValue, derivative))
}

func buildErosionOffset(lowValley, hill, tallHill, mountainFactor, plain, swamp float32, includeExtremeHills, saddle bool) *terrainSpline {
	veryLow := mountainRidgeSpline(lerp32(mountainFactor, 0.6, 1.5), saddle)
	low := mountainRidgeSpline(lerp32(mountainFactor, 0.6, 1), saddle)
	mountains := mountainRidgeSpline(mountainFactor, saddle)
	widePlateau := ridgeSpline(float32(lowValley-0.15), float32(float32(0.5*mountainFactor)), float32(0.5*mountainFactor), float32(0.5*mountainFactor), float32(0.6*mountainFactor), 0.5)
	narrowPlateau := ridgeSpline(lowValley, float32(plain*mountainFactor), float32(hill*mountainFactor), float32(0.5*mountainFactor), float32(0.6*mountainFactor), 0.5)
	plains := ridgeSpline(lowValley, plain, plain, hill, tallHill, 0.5)
	swamps := ridgeSpline(-0.02, swamp, swamp, hill, tallHill, 0)
	if includeExtremeHills {
		extreme := spline(ridges, scalar(-1, lowValley, 0), nested(-0.4, plains), scalar(0, float32(tallHill+0.07), 0))
		return spline(erosion,
			nested(-0.85, veryLow), nested(-0.7, low), nested(-0.4, mountains), nested(-0.35, widePlateau),
			nested(-0.1, narrowPlateau), nested(0.2, plains), nested(0.4, plains), nested(0.45, extreme),
			nested(0.55, extreme), nested(0.58, plains), nested(0.7, swamps),
		)
	}
	return spline(erosion,
		nested(-0.85, veryLow), nested(-0.7, low), nested(-0.4, mountains), nested(-0.35, widePlateau),
		nested(-0.1, narrowPlateau), nested(0.2, plains), nested(0.7, swamps),
	)
}

func buildOffset() *terrainSpline {
	beach := buildErosionOffset(-0.15, 0, 0, 0.1, 0, -0.03, false, false)
	low := buildErosionOffset(-0.1, 0.03, 0.1, 0.1, 0.01, -0.03, false, false)
	mid := buildErosionOffset(-0.1, 0.03, 0.1, 0.7, 0.01, -0.03, true, true)
	high := buildErosionOffset(-0.05, 0.03, 0.1, 1.0, 0.01, 0.01, true, true)
	return spline(continentalness,
		scalar(-1.1, 0.044, 0), scalar(-1.02, -0.2222, 0), scalar(-0.51, -0.2222, 0),
		scalar(-0.44, -0.12, 0), scalar(-0.18, -0.12, 0), nested(-0.16, beach), nested(-0.15, beach),
		nested(-0.1, low), nested(0.25, mid), nested(1, high),
	)
}

var overworldOffsetSpline = buildOffset()
var overworldFactorSpline = buildFactor()
var overworldJaggednessSpline = buildJaggedness()

// TerrainOffset returns the raw vanilla Overworld offset spline value.
func TerrainOffset(continentalness, erosion, weirdness float32) float32 {
	ridges := peaksAndValleys(weirdness)
	return overworldOffsetSpline.sample(continentalness, erosion, weirdness, ridges)
}

// TerrainFactor returns the vanilla Overworld terrain factor.
func TerrainFactor(continentalness, erosion, weirdness float32) float32 {
	ridges := peaksAndValleys(weirdness)
	return overworldFactorSpline.sample(continentalness, erosion, weirdness, ridges)
}

// TerrainJaggedness returns the vanilla Overworld terrain jaggedness.
func TerrainJaggedness(continentalness, erosion, weirdness float32) float32 {
	ridges := peaksAndValleys(weirdness)
	return overworldJaggednessSpline.sample(continentalness, erosion, weirdness, ridges)
}
