package bedrock

import "math"

// BlendedNoise is the base terrain noise used by modern Bedrock. It is
// immutable after construction and safe for concurrent sampling.
type BlendedNoise struct {
	minLimit [16]improvedNoise
	maxLimit [16]improvedNoise
	main     [8]improvedNoise
}

// NewBlendedNoise constructs the native terrain noise stacks from the named
// world-seed stream. Native construction appends the two 16-octave limit
// stacks followed by the 8-octave main stack, consuming one shared stream.
func NewBlendedNoise(seed int64) *BlendedNoise {
	n := new(BlendedNoise)
	r := NewRandomFactory(seed).fromName([]byte("minecraft:terrain"))
	for i := range n.minLimit {
		n.minLimit[i].init(&r, 1, 1)
	}
	for i := range n.maxLimit {
		n.maxLimit[i].init(&r, 1, 1)
	}
	for i := range n.main {
		n.main[i].init(&r, 1, 1)
	}
	return n
}

// Sample evaluates the base terrain density at block coordinates. Its output
// uses Bedrock's native density units (128 times the Java density units).
func (n *BlendedNoise) Sample(x, y, z int32) float32 {
	// The native density composer samples horizontal quart coordinates and
	// vertical 8-block cells before interpolating the resulting volume.
	qx := float32(x) * 0.25
	qy := float32(y) * 0.125
	qz := float32(z) * 0.25

	const (
		limitScaleX = float32(684.412)
		limitScaleY = float32(684.412)
		mainScaleX  = float32(8.55515)
		mainScaleY  = float32(4.277575)
		limitUnit   = float32(1.0 / 512.0)
	)
	minValue := blendedOctaves(n.minLimit[:], qx, qy, qz, limitScaleX, limitScaleY)
	maxValue := blendedOctaves(n.maxLimit[:], qx, qy, qz, limitScaleX, limitScaleY)
	mainValue := blendedOctaves(n.main[:], qx, qy, qz, mainScaleX, mainScaleY)

	minValue = float32(minValue * limitUnit)
	maxValue = float32(maxValue * limitUnit)
	choice := float32(float32(float32(mainValue/10)+1) * 0.5)
	if !(choice >= 0) {
		return minValue
	}
	if choice > 1 {
		return maxValue
	}
	return lerp32(choice, minValue, maxValue)
}

func blendedOctaves(octaves []improvedNoise, x, y, z, scaleXZ, scaleY float32) float32 {
	var value float32
	frequency := float32(1)
	for i := range octaves {
		layerScaleXZ := float32(frequency * scaleXZ)
		layerScaleY := float32(frequency * scaleY)
		amplitude := float32(1 / frequency)
		layer := octaves[i].sampleBlended(x, y, z, layerScaleXZ, layerScaleY, amplitude)
		value = float32(value + layer)
		frequency = float32(frequency * 0.5)
	}
	return value
}

func (n *improvedNoise) sampleBlended(x, y, z, scaleXZ, scaleY, amplitude float32) float32 {
	x = float32(float32(x*scaleXZ) + n.x)
	y = float32(float32(y*scaleY) + n.y)
	z = float32(float32(z*scaleXZ) + n.z)

	floorX := float32Floor(x)
	floorY := float32Floor(y)
	floorZ := float32Floor(z)
	ix, iy, iz := floor32(floorX), floor32(floorY), floor32(floorZ)
	x, y, z = x-floorX, y-floorY, z-floorZ

	// The native SmearedPerlin configuration sets its y-origin field to zero.
	// In the offset's own Y cell, gradients use the offset's fractional part;
	// elsewhere they wrap the relative Y by the octave's scale. The fade still
	// uses the unmodified relative coordinate.
	gradientY := y
	offsetCellY := int32(n.y)
	if offsetCellY == iy {
		gradientY = n.y - float32(offsetCellY)
	} else {
		gradientY = float32(y - float32(floor32(float32(y/scaleY)))*scaleY)
	}

	xIndex, yIndex, zIndex := uint8(ix), uint8(iy), uint8(iz)
	a := n.permutation[xIndex]
	b := n.permutation[xIndex+1]
	aa := n.permutation[a+yIndex]
	ab := n.permutation[a+yIndex+1]
	ba := n.permutation[b+yIndex]
	bb := n.permutation[b+yIndex+1]
	sx, sy, sz := fade32(x), fade32(y), fade32(z)

	low := lerp32(sy,
		lerp32(sx, gradient(n.permutation[aa+zIndex], x, gradientY, z), gradient(n.permutation[ba+zIndex], x-1, gradientY, z)),
		lerp32(sx, gradient(n.permutation[ab+zIndex], x, gradientY-1, z), gradient(n.permutation[bb+zIndex], x-1, gradientY-1, z)),
	)
	high := lerp32(sy,
		lerp32(sx, gradient(n.permutation[aa+zIndex+1], x, gradientY, z-1), gradient(n.permutation[ba+zIndex+1], x-1, gradientY, z-1)),
		lerp32(sx, gradient(n.permutation[ab+zIndex+1], x, gradientY-1, z-1), gradient(n.permutation[bb+zIndex+1], x-1, gradientY-1, z-1)),
	)
	return float32(lerp32(sz, low, high) * amplitude)
}

func float32Floor(value float32) float32 {
	return float32(math.Floor(float64(value)))
}
