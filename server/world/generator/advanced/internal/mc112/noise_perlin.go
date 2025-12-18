package mc112

// NoisePerlin is a port of net.minecraft.world.gen.NoiseGeneratorPerlin.
//
// It combines multiple simplex noise generators into an octave-like noise source.
type NoisePerlin struct {
	levels []*NoiseSimplex
}

// NewNoisePerlin returns a perlin noise generator using r for initialisation.
func NewNoisePerlin(r *Rand, octaves int) *NoisePerlin {
	levels := make([]*NoiseSimplex, octaves)
	for i := 0; i < octaves; i++ {
		levels[i] = NewNoiseSimplex(r)
	}
	return &NoisePerlin{levels: levels}
}

// GetValue returns noise for coordinates x,y.
func (n *NoisePerlin) GetValue(x, y float64) float64 {
	d0 := 0.0
	d1 := 1.0
	for _, level := range n.levels {
		d0 += level.GetValue(x*d1, y*d1) / d1
		d1 /= 2.0
	}
	return d0
}

// GetRegion fills/allocates dst and returns it. The slice is written with xSize*ySize values.
func (n *NoisePerlin) GetRegion(dst []float64, xOffset, yOffset float64, xSize, ySize int, xScale, yScale, amplitude float64) []float64 {
	total := xSize * ySize
	if dst == nil {
		dst = make([]float64, total)
	} else if len(dst) != total {
		dst = make([]float64, total)
	} else {
		for i := range dst {
			dst[i] = 0
		}
	}

	// Matches NoiseGeneratorPerlin.getRegion in Minecraft Java 1.12:
	// d0 is multiplied by the 'noiseScale' parameter each octave, while d1 is halved (0.5) each octave.
	// The simplex amplitude is fixed to 0.55/d1.
	d1 := 1.0
	d0 := 1.0
	for _, level := range n.levels {
		level.Add(dst, xOffset, yOffset, xSize, ySize, xScale*d0*d1, yScale*d0*d1, 0.55/d1)
		d0 *= amplitude
		d1 *= 0.5
	}
	return dst
}
