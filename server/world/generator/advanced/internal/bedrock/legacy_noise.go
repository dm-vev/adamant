package bedrock

// LegacyImprovedNoise is the compact legacy Perlin sampler used by Bedrock.
type LegacyImprovedNoise struct {
	x, y, z float32
	perm    [256]uint8
	flags   uint8
	smearY  bool
	yParam  float32
	enabled bool
}

// Init initializes the native three-dimensional Perlin sampler from r.
func (n *LegacyImprovedNoise) Init(r *LegacyRandom) {
	n.x = r.NextFloat() * 256
	n.y = r.NextFloat() * 256
	n.z = r.NextFloat() * 256
	for i := range 256 {
		n.perm[i] = uint8(i)
	}
	for i := range 256 {
		j := i + int(r.NextInt(uint32(256-i)))
		n.perm[i], n.perm[j] = n.perm[j], n.perm[i]
	}
	n.flags = 1
	n.smearY = true
	n.yParam = 0
	n.enabled = true
}

// Fill adds native bulk Perlin samples multiplied by gain to dst in x/z/y
// order. Counts, offsets, scales, and steps are ordered x/y/z. Each axis
// visits indices i*step for i in [0,count), and dst must hold countX*countY*countZ values.
// The vertical cache resets at each call, including across x/z columns. Gain
// is the caller's output multiplier, not the native sampler's reciprocal input.
func (n *LegacyImprovedNoise) Fill(dst []float32, offsets [3]float32, counts [3]int, scales [3]float32, gain float32, steps [3]int) {
	xCount, yCount, zCount := counts[0]*steps[0], counts[1]*steps[1], counts[2]*steps[2]
	output := 0
	previousY := uint32(0xffffffff)
	var cached [4]float32
	yBase := float32(scales[1]*n.yParam) + n.y
	yBaseFrac := yBase - float32(int32(yBase))

	for xIndex := 0; xIndex < xCount; xIndex += steps[0] {
		x := float32(float32(float32(xIndex)+offsets[0])*scales[0]) + n.x
		floorX := float32Floor(x)
		ix := floor32(x)
		xFrac := x - floorX
		for zIndex := 0; zIndex < zCount; zIndex += steps[2] {
			z := float32(float32(float32(zIndex)+offsets[2])*scales[2]) + n.z
			floorZ := float32Floor(z)
			iz := floor32(z)
			zFrac := z - floorZ
			for yIndex := 0; yIndex < yCount; yIndex += steps[1] {
				y := float32(float32(float32(yIndex)+offsets[1])*scales[1]) + n.y
				floorY := float32Floor(y)
				iy := floor32(y)
				rawYFrac := y - floorY
				yFrac := rawYFrac

				if n.enabled && counts[1] == 1 {
					dst[output] = float32(dst[output] + n.sample2D(xFrac, zFrac, ix, iz)*gain)
					output++
					continue
				}

				currentY := uint32(uint8(iy))
				recompute := true
				if n.flags == 1 {
					if !n.smearY || int32(yBase) != int32(y) {
						yFrac = yBaseFrac
						if currentY != previousY {
							smearCell := float32Floor(rawYFrac / scales[1])
							yFrac = float32(rawYFrac - smearCell*scales[1])
							previousY = currentY
						} else {
							recompute = false
						}
					} else {
						yFrac = yBaseFrac
					}
				}

				if recompute {
					cached = n.sampleXInterpolants(xFrac, yFrac, zFrac, ix, iy, iz)
				}
				dst[output] = float32(dst[output] + n.interpolate(cached, rawYFrac, zFrac)*gain)
				output++
			}
		}
	}
}


func (n *LegacyImprovedNoise) sample2D(x, z float32, ix, iz int32) float32 {
	xi, zi := uint8(ix), uint8(iz)
	left, right := n.perm[xi], n.perm[xi+1]
	left = n.perm[left] + zi
	right = n.perm[right] + zi
	xFade, zFade := fade32(x), fade32(z)
	low := lerp32(xFade,
		gradient(n.perm[left], x, 0, z),
		gradient(n.perm[right], x-1, 0, z),
	)
	high := lerp32(xFade,
		gradient(n.perm[left+1], x, 0, z-1),
		gradient(n.perm[right+1], x-1, 0, z-1),
	)
	return lerp32(zFade, low, high)
}


func (n *LegacyImprovedNoise) sampleXInterpolants(x, y, z float32, ix, iy, iz int32) [4]float32 {
	xi, yi, zi := uint8(ix), uint8(iy), uint8(iz)
	a := n.perm[xi]
	b := n.perm[xi+1]
	aa, ab := n.perm[uint8(a+yi)], n.perm[uint8(a+yi+1)]
	ba, bb := n.perm[uint8(b+yi)], n.perm[uint8(b+yi+1)]
	aa, ab = aa+zi, ab+zi
	ba, bb = ba+zi, bb+zi
	xFade := fade32(x)
	var out [4]float32
	out[0] = lerp32(xFade, gradient(n.perm[aa], x, y, z), gradient(n.perm[ba], x-1, y, z))
	out[1] = lerp32(xFade, gradient(n.perm[ab], x, y-1, z), gradient(n.perm[bb], x-1, y-1, z))
	out[2] = lerp32(xFade, gradient(n.perm[aa+1], x, y, z-1), gradient(n.perm[ba+1], x-1, y, z-1))
	out[3] = lerp32(xFade, gradient(n.perm[ab+1], x, y-1, z-1), gradient(n.perm[bb+1], x-1, y-1, z-1))
	return out
}

func (n *LegacyImprovedNoise) interpolate(cached [4]float32, y, z float32) float32 {
	yFade, zFade := fade32(y), fade32(z)
	low := lerp32(yFade, cached[0], cached[1])
	high := lerp32(yFade, cached[2], cached[3])
	return lerp32(zFade, low, high)
}
