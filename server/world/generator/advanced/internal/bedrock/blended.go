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

// Fill evaluates all 5×41×5 native density vertices for a chunk. Bulk Perlin
// retains gradient interpolation until the Y lattice cell changes: sampling
// each vertex independently does not reproduce its floating-point results.
func (n *BlendedNoise) Fill(chunkX, chunkZ int32, values *[densityGridSize]float32) {
	var maximum, main [densityGridSize]float32
	clear(values[:])
	qx, qz := float32(chunkX*4), float32(chunkZ*4)
	blendedOctaves(n.minLimit[:], qx, qz, 684.412, 684.412, values)
	blendedOctaves(n.maxLimit[:], qx, qz, 684.412, 684.412, &maximum)
	blendedOctaves(n.main[:], qx, qz, 8.55515, 4.277575, &main)
	for i := range values {
		low, high := float32(values[i]/512), float32(maximum[i]/512)
		choice := float32(float32(float32(main[i]/10)+1) * 0.5)
		value := low
		if choice >= 0 {
			value = high
			if choice <= 1 {
				value = lerp32(choice, low, high)
			}
		}
		values[i] = value
	}
}

func blendedOctaves(octaves []improvedNoise, x, z, scaleXZ, scaleY float32, values *[densityGridSize]float32) {
	frequency := float32(1)
	for i := range octaves {
		octaves[i].fillBlended(x, z, float32(frequency*scaleXZ), float32(frequency*scaleY), float32(1/frequency), values)
		frequency = float32(frequency * 0.5)
	}
}

func (n *improvedNoise) fillBlended(originX, originZ, scaleXZ, scaleY, amplitude float32, values *[densityGridSize]float32) {
	previousY := int32(-1)
	var low0, low1, high0, high1 float32
	for dx := range 5 {
		x := float32(float32(float32(originX+float32(dx))*scaleXZ) + n.x)
		floorX := float32Floor(x)
		x = float32(x - floorX)
		ix := uint8(floor32(floorX))
		sx := fade32(x)
		for dz := range 5 {
			z := float32(float32(float32(originZ+float32(dz))*scaleXZ) + n.z)
			floorZ := float32Floor(z)
			z = float32(z - floorZ)
			iz := uint8(floor32(floorZ))
			sz := fade32(z)
			base := (dx*5 + dz) * densityColumnSize
			for dy := range densityColumnSize {
				absoluteY := float32(float32(float32(dy-8)*scaleY) + n.y)
				floorY := float32Floor(absoluteY)
				y := float32(absoluteY - floorY)
				iy := int32(uint8(floor32(floorY)))
				gradientY := float32(n.y - float32(int32(n.y)))
				offsetCell := int32(n.y) == int32(absoluteY)
				if offsetCell || iy != previousY {
					if !offsetCell {
						gradientY = float32(y - float32(float32Floor(float32(y/scaleY))*scaleY))
					}
					previousY = iy
					a, b := n.permutation[ix], n.permutation[ix+1]
					aa, ab := n.permutation[a+uint8(iy)], n.permutation[a+uint8(iy)+1]
					ba, bb := n.permutation[b+uint8(iy)], n.permutation[b+uint8(iy)+1]
					low0 = lerp32(sx, gradient(n.permutation[aa+iz], x, gradientY, z), gradient(n.permutation[ba+iz], x-1, gradientY, z))
					low1 = lerp32(sx, gradient(n.permutation[ab+iz], x, gradientY-1, z), gradient(n.permutation[bb+iz], x-1, gradientY-1, z))
					high0 = lerp32(sx, gradient(n.permutation[aa+iz+1], x, gradientY, z-1), gradient(n.permutation[ba+iz+1], x-1, gradientY, z-1))
					high1 = lerp32(sx, gradient(n.permutation[ab+iz+1], x, gradientY-1, z-1), gradient(n.permutation[bb+iz+1], x-1, gradientY-1, z-1))
				}
				sy := fade32(y)
				value := float32(lerp32(sz, lerp32(sy, low0, low1), lerp32(sy, high0, high1)) * amplitude)
				values[base+dy] = float32(values[base+dy] + value)
			}
		}
	}
}

func float32Floor(value float32) float32 {
	return float32(math.Floor(float64(value)))
}
