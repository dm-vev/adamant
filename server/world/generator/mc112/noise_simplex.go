package mc112

import "math"

// noiseSimplex is a port of net.minecraft.world.gen.NoiseGeneratorSimplex (2D subset).
type noiseSimplex struct {
	p  [512]int
	xo float64
	yo float64
	zo float64
}

var (
	simplexGrad3 = [12][3]int{
		{1, 1, 0}, {-1, 1, 0}, {1, -1, 0}, {-1, -1, 0},
		{1, 0, 1}, {-1, 0, 1}, {1, 0, -1}, {-1, 0, -1},
		{0, 1, 1}, {0, -1, 1}, {0, 1, -1}, {0, -1, -1},
	}
	sqrt3 = math.Sqrt(3.0)
	f2    = 0.5 * (sqrt3 - 1.0)
	g2    = (3.0 - sqrt3) / 6.0
)

func newNoiseSimplex(r *javaRand) *noiseSimplex {
	n := &noiseSimplex{
		xo: r.Float64() * 256.0,
		yo: r.Float64() * 256.0,
		zo: r.Float64() * 256.0,
	}
	for i := 0; i < 256; i++ {
		n.p[i] = i
	}
	for l := 0; l < 256; l++ {
		j := int(r.Intn(int32(256-l))) + l
		k := n.p[l]
		n.p[l] = n.p[j]
		n.p[j] = k
		n.p[l+256] = n.p[l]
	}
	return n
}

func simplexFastFloor(value float64) int {
	if value > 0.0 {
		return int(value)
	}
	return int(value) - 1
}

func simplexDot(g [3]int, x, y float64) float64 {
	return float64(g[0])*x + float64(g[1])*y
}

func (n *noiseSimplex) getValue(x, y float64) float64 {
	d4 := (x + y) * f2
	i := simplexFastFloor(x + d4)
	j := simplexFastFloor(y + d4)

	d6 := float64(i+j) * g2
	d7 := float64(i) - d6
	d8 := float64(j) - d6
	d9 := x - d7
	d10 := y - d8

	var k, l int
	if d9 > d10 {
		k, l = 1, 0
	} else {
		k, l = 0, 1
	}

	d11 := d9 - float64(k) + g2
	d12 := d10 - float64(l) + g2
	d13 := d9 - 1.0 + 2.0*g2
	d14 := d10 - 1.0 + 2.0*g2

	i1 := i & 255
	j1 := j & 255

	k1 := n.p[i1+n.p[j1]] % 12
	l1 := n.p[i1+k+n.p[j1+l]] % 12
	i2 := n.p[i1+1+n.p[j1+1]] % 12

	d15 := 0.5 - d9*d9 - d10*d10
	var d0 float64
	if d15 < 0.0 {
		d0 = 0.0
	} else {
		d15 *= d15
		d0 = d15 * d15 * simplexDot(simplexGrad3[k1], d9, d10)
	}

	d16 := 0.5 - d11*d11 - d12*d12
	var d1 float64
	if d16 < 0.0 {
		d1 = 0.0
	} else {
		d16 *= d16
		d1 = d16 * d16 * simplexDot(simplexGrad3[l1], d11, d12)
	}

	d17 := 0.5 - d13*d13 - d14*d14
	var d2 float64
	if d17 < 0.0 {
		d2 = 0.0
	} else {
		d17 *= d17
		d2 = d17 * d17 * simplexDot(simplexGrad3[i2], d13, d14)
	}

	return 70.0 * (d0 + d1 + d2)
}

// add adds simplex noise to dst, matching NoiseGeneratorSimplex.add in Minecraft 1.12.
// dst is treated as a 2D array with size xSize*ySize, written row-major.
func (n *noiseSimplex) add(dst []float64, xOffset, yOffset float64, xSize, ySize int, xScale, yScale, amplitude float64) {
	i := 0
	for y := 0; y < ySize; y++ {
		d0 := (yOffset+float64(y))*yScale + n.yo

		for x := 0; x < xSize; x++ {
			d1 := (xOffset+float64(x))*xScale + n.xo

			d5 := (d1 + d0) * f2
			l := simplexFastFloor(d1 + d5)
			i1 := simplexFastFloor(d0 + d5)

			d6 := float64(l+i1) * g2
			d7 := float64(l) - d6
			d8 := float64(i1) - d6
			d9 := d1 - d7
			d10 := d0 - d8

			var j1, k1 int
			if d9 > d10 {
				j1, k1 = 1, 0
			} else {
				j1, k1 = 0, 1
			}

			d11 := d9 - float64(j1) + g2
			d12 := d10 - float64(k1) + g2
			d13 := d9 - 1.0 + 2.0*g2
			d14 := d10 - 1.0 + 2.0*g2

			l1 := l & 255
			i2 := i1 & 255
			j2 := n.p[l1+n.p[i2]] % 12
			k2 := n.p[l1+j1+n.p[i2+k1]] % 12
			l2 := n.p[l1+1+n.p[i2+1]] % 12

			d15 := 0.5 - d9*d9 - d10*d10
			var d2 float64
			if d15 < 0.0 {
				d2 = 0.0
			} else {
				d15 *= d15
				d2 = d15 * d15 * simplexDot(simplexGrad3[j2], d9, d10)
			}

			d16 := 0.5 - d11*d11 - d12*d12
			var d3 float64
			if d16 < 0.0 {
				d3 = 0.0
			} else {
				d16 *= d16
				d3 = d16 * d16 * simplexDot(simplexGrad3[k2], d11, d12)
			}

			d17 := 0.5 - d13*d13 - d14*d14
			var d4 float64
			if d17 < 0.0 {
				d4 = 0.0
			} else {
				d17 *= d17
				d4 = d17 * d17 * simplexDot(simplexGrad3[l2], d13, d14)
			}

			dst[i] += 70.0 * (d2 + d3 + d4) * amplitude
			i++
		}
	}
}
