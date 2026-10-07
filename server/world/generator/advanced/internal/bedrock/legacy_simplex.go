package bedrock


// LegacySimplexNoise is Bedrock's 0x80c simplex-noise primitive. The
// permutation and offsets are fixed at construction, so it is safe for
// concurrent sampling.
type LegacySimplexNoise struct {
	offsetX, offsetY, offsetZ float32
	permutation               [512]uint8
}

// Init consumes three random floats for offsets followed by the native
// Fisher-Yates permutation draws.
func (n *LegacySimplexNoise) Init(random *LegacyRandom) {
	x, y, z := random.NextFloat(), random.NextFloat(), random.NextFloat()
	n.offsetX = float32(z * 256)
	n.offsetY = float32(y * 256)
	n.offsetZ = float32(x * 256)
	for i := range 256 {
		n.permutation[i] = uint8(i)
	}
	for i := range 256 {
		j := i + int(random.NextInt(uint32(256-i)))
		n.permutation[i], n.permutation[j] = n.permutation[j], n.permutation[i]
		n.permutation[i+256] = n.permutation[i]
	}
}

// Sample2D returns the native simplex sample at x,z. Coordinates supplied by
// callers already include the instance offsets.
func (n *LegacySimplexNoise) Sample2D(x, z float32) float32 {
	f2 := float32(0.3660254037844386)
	g2 := float32(0.21132486540518713)
	skew := float32(float32(x+z) * f2)
	cellX := float32(x + skew)
	cellZ := float32(z + skew)
	i := float32Floor(cellX)
	j := float32Floor(cellZ)
	unskew := float32(float32(i+j) * g2)
	x0 := float32(x - float32(i-unskew))
	z0 := float32(z - float32(j-unskew))
	var i1, j1 float32
	if x0 > z0 {
		i1 = 1
	} else {
		j1 = 1
	}
	x1 := float32(float32(x0-i1) + g2)
	z1 := float32(float32(z0-j1) + g2)
	x2 := float32(float32(x0-1) + float32(2*g2))
	z2 := float32(float32(z0-1) + float32(2*g2))
	maskI, maskJ := uint8(uint32(i)), uint8(uint32(j))
	p0 := n.permutation[int(n.permutation[maskJ])+int(maskI)]
	p1 := n.permutation[int(n.permutation[maskJ+uint8(j1)])+int(maskI+uint8(i1))]
	p2 := n.permutation[int(n.permutation[maskJ+1])+int(maskI+1)]
	return float32(float32(float32(simplexCorner2D(p0, x0, z0)+simplexCorner2D(p1, x1, z1))+simplexCorner2D(p2, x2, z2)) * 70)
}

func simplexCorner2D(hash uint8, x, z float32) float32 {
	t := float32(float32(0.5 - float32(x*x)) - float32(z*z))
	if t < 0 {
		return 0
	}
	gradients := [12][2]float32{
		{1, 1}, {-1, 1}, {1, -1}, {-1, -1},
		{1, 0}, {-1, 0}, {1, 0}, {-1, 0},
		{0, 1}, {0, -1}, {0, 1}, {0, -1},
	}
	g := gradients[hash%12]
	t2 := float32(t * t)
	return float32(float32(float32(g[0]*x+g[1]*z) * t2) * t2)
}

// LegacySimplexStack is the native octave accumulation used by Nether climate
// fields. Fill writes row-major X/Z samples; the output buffer must have 256
// elements.
type LegacySimplexStack [4]LegacySimplexNoise

func (s *LegacySimplexStack) Init(random *LegacyRandom) {
	for i := range s {
		s[i].Init(random)
	}
}

func (s *LegacySimplexStack) Fill(dst []float32, chunkX, chunkZ int32) {
	for z := range 16 {
		for x := range 16 {
			worldX := float32(float32(float32(x)+float32(chunkX<<4)) * float32(1.0/16.0))
			worldZ := float32(float32(float32(z)+float32(chunkZ<<4)) * float32(1.0/16.0))
			var sample float32
			frequency := float32(1)
			for i := range s {
				noise := &s[i]
				px := float32(worldX + noise.offsetX)
				pz := float32(worldZ + noise.offsetY)
				sample = float32(sample + float32(noise.Sample2D(px, pz)*float32(0.55/frequency)))
				frequency = float32(frequency * 0.5)
			}
			dst[z*16+x] = sample
		}
	}
}

