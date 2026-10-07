package bedrock

// EndSimplex is the seed-scoped 2D simplex sampler used by the End island
// density function. It shares Bedrock's legacy MT stream with the End octave
// stacks and is immutable after construction.
type EndSimplex struct {
	x, y float32
	perm [512]int32
}

// NewEndSimplex initializes the native simplex permutation from r.
func NewEndSimplex(r *LegacyRandom) EndSimplex {
	var n EndSimplex
	n.x = r.NextFloat() * 256
	n.y = r.NextFloat() * 256
	for i := 0; i < 256; i++ {
		n.perm[i] = int32(i)
	}
	for i := 0; i < 256; i++ {
		j := i + int(r.NextInt(uint32(256-i)))
		n.perm[i], n.perm[j] = n.perm[j], n.perm[i]
	}
	copy(n.perm[256:], n.perm[:256])
	return n
}

// Sample returns the native float32 simplex value for section coordinates.
func (n *EndSimplex) Sample(x, y float32) float32 {
	x = float32(x + n.x)
	y = float32(y + n.y)
	const f2 float32 = 0.36602540378443864676
	const g2 float32 = 0.21132486540518711775
	s := float32((x + y) * f2)
	i := floor32(float32(x + s))
	j := floor32(float32(y + s))
	t := float32(float32(i+j) * g2)
	x0 := float32(x - (float32(i) - t))
	y0 := float32(y - (float32(j) - t))
	i1, j1 := 0, 0
	if x0 > y0 {
		i1 = 1
	} else {
		j1 = 1
	}
	x1 := float32(x0 - float32(i1) + g2)
	y1 := float32(y0 - float32(j1) + g2)
	x2 := float32(x0 - 1 + 2*g2)
	y2 := float32(y0 - 1 + 2*g2)
	permutation := func(x, y int32) int32 {
		return n.perm[uint8(x)+uint8(n.perm[uint8(y)])]
	}
	corner := func(hash int32, x, y float32) float32 {
		t := float32(0.5 - x*x - y*y)
		if t < 0 {
			return 0
		}
		gradient := endSimplexGrad3[uint8(hash)%12]
		t = float32(t * t)
		return float32(float32(t*t) * float32(float32(gradient[0])*x+float32(gradient[1])*y))
	}
	ii, jj := int32(uint8(i)), int32(uint8(j))
	value := corner(permutation(ii, jj), x0, y0) + corner(permutation(ii+int32(i1), jj+int32(j1)), x1, y1) + corner(permutation(ii+1, jj+1), x2, y2)
	return float32(value * 70)
}

var endSimplexGrad3 = [12][3]int8{
	{1, 1, 0}, {-1, 1, 0}, {1, -1, 0}, {-1, -1, 0},
	{1, 0, 1}, {-1, 0, 1}, {1, 0, -1}, {-1, 0, -1},
	{0, 1, 1}, {0, -1, 1}, {0, 1, -1}, {0, -1, -1},
}
