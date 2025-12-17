package mc112

// NoiseOctaves is a port of net.minecraft.world.gen.NoiseGeneratorOctaves.
type NoiseOctaves struct {
	generators []*noiseImproved
}

// NewNoiseOctaves returns an octaves noise generator using r for initialisation.
func NewNoiseOctaves(r *Rand, octaves int) *NoiseOctaves {
	gens := make([]*noiseImproved, octaves)
	for i := 0; i < octaves; i++ {
		gens[i] = newNoiseImproved(r)
	}
	return &NoiseOctaves{generators: gens}
}

// GenerateNoiseOctaves fills/allocates noiseArray and returns it. The slice is written with xSize*ySize*zSize values.
func (n *NoiseOctaves) GenerateNoiseOctaves(noiseArray []float64, xOffset, yOffset, zOffset, xSize, ySize, zSize int, xScale, yScale, zScale float64) []float64 {
	total := xSize * ySize * zSize
	if noiseArray == nil {
		noiseArray = make([]float64, total)
	} else {
		if len(noiseArray) != total {
			noiseArray = make([]float64, total)
		} else {
			for i := range noiseArray {
				noiseArray[i] = 0
			}
		}
	}

	d3 := 1.0
	for _, gen := range n.generators {
		d0 := float64(xOffset) * d3 * xScale
		d1 := float64(yOffset) * d3 * yScale
		d2 := float64(zOffset) * d3 * zScale

		k := lfloor(d0)
		l := lfloor(d2)

		d0 -= float64(k)
		d2 -= float64(l)

		k %= 16777216
		l %= 16777216

		d0 += float64(k)
		d2 += float64(l)

		gen.populateNoiseArray(noiseArray, d0, d1, d2, xSize, ySize, zSize, xScale*d3, yScale*d3, zScale*d3, d3)
		d3 /= 2.0
	}
	return noiseArray
}

func lfloor(v float64) int64 {
	i := int64(v)
	if v < float64(i) {
		return i - 1
	}
	return i
}
