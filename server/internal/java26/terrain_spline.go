package java26

// terrainSplineNode and terrainSplinePoint hold the immutable TerrainProvider
// graph in contiguous arrays. Sampling allocates no per-coordinate objects.
type terrainSplineNode struct {
	coordinate   uint8
	start, count uint16
	value        float32
}

type terrainSplinePoint struct {
	location, derivative float32
	child                uint16
}

// TerrainShape samples the Java Overworld offset, factor and jaggedness splines.
// Offset is the raw spline value, before NoiseRouterData's global -0.50375 bias.
func TerrainShape(continents, erosion, weirdness float32) (offset, factor, jaggedness float32) {
	coordinates := [4]float32{continents, erosion, PeaksAndValleys(weirdness), weirdness}
	return sampleTerrainSpline(terrainSplineRoots[0], &coordinates), sampleTerrainSpline(terrainSplineRoots[1], &coordinates), sampleTerrainSpline(terrainSplineRoots[2], &coordinates)
}

// PeaksAndValleys folds the Java ridge noise into its terrain spline coordinate.
func PeaksAndValleys(weirdness float32) float32 {
	return -(terrainAbs(terrainAbs(weirdness)-0.6666667) - 0.33333334) * 3
}

func sampleTerrainSpline(index uint16, coordinates *[4]float32) float32 {
	node := &terrainSplineNodes[index]
	if node.count == 0 {
		return node.value
	}
	input := coordinates[node.coordinate]
	points := terrainSplinePoints[int(node.start) : int(node.start)+int(node.count)]
	low, high := 0, len(points)
	for low < high {
		middle := (low + high) >> 1
		if input < points[middle].location {
			high = middle
		} else {
			low = middle + 1
		}
	}
	left := low - 1
	if left < 0 || left == len(points)-1 {
		point := &points[max(left, 0)]
		value := sampleTerrainSpline(point.child, coordinates)
		if point.derivative == 0 {
			return value
		}
		return value + point.derivative*(input-point.location)
	}
	p, q := &points[left], &points[left+1]
	width := q.location - p.location
	t := (input - p.location) / width
	a, b := sampleTerrainSpline(p.child, coordinates), sampleTerrainSpline(q.child, coordinates)
	difference := b - a
	pDerivative := p.derivative*width - difference
	qDerivative := -q.derivative*width + difference
	return terrainLerp(t, a, b) + t*(1-t)*terrainLerp(t, pDerivative, qDerivative)
}

func terrainAbs(value float32) float32 {
	if value < 0 {
		return -value
	}
	return value
}

func terrainLerp(delta, start, end float32) float32 {
	return start + delta*(end-start)
}
