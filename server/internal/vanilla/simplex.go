package vanilla

import "math"

// SimplexNoise shares Java GradientNoise's permutation and offset construction.
type SimplexNoise struct{ p Perlin }

func NewSimplexNoise(random *Random) *SimplexNoise {
	return &SimplexNoise{p: newPerlin(random)}
}

// NewSimplexNoiseWithoutOffset is SimplexNoise(random, true), used by the End.
// The three random double draws are retained even though their offsets are zero.
func NewSimplexNoiseWithoutOffset(random *Random) *SimplexNoise {
	noise := NewSimplexNoise(random)
	noise.p.ox, noise.p.oy, noise.p.oz = 0, 0, 0
	return noise
}

func NewLegacySimplexNoise(seed int64) *SimplexNoise {
	random := NewLegacyRandom(seed)
	return NewSimplexNoise(&random)
}

func simplexCorner(index int, x, y, z, base float64) float64 {
	t := base - x*x - y*y - z*z
	if t < 0 {
		return 0
	}
	t *= t
	g := gradients[index]
	return t * t * (float64(g[0])*x + float64(g[1])*y + float64(g[2])*z)
}

func (s *SimplexNoise) Sample2D(x, y float64) float64 {
	const sqrt3 = 1.7320508075688772
	const f2 = 0.5 * (sqrt3 - 1)
	const g2 = (3 - sqrt3) / 6
	x, y = x+s.p.ox, y+s.p.oy
	skew := (x + y) * f2
	i, j := int(math.Floor(x+skew)), int(math.Floor(y+skew))
	unskew := float64(i+j) * g2
	x0, y0 := x-(float64(i)-unskew), y-(float64(j)-unskew)
	i1, j1 := 0, 1
	if x0 > y0 {
		i1, j1 = 1, 0
	}
	x1, y1 := x0-float64(i1)+g2, y0-float64(j1)+g2
	x2, y2 := x0-1+2*g2, y0-1+2*g2
	ii, jj := i&255, j&255
	gi0 := s.p.permute(ii+s.p.permute(jj)) % 12
	gi1 := s.p.permute(ii+i1+s.p.permute(jj+j1)) % 12
	gi2 := s.p.permute(ii+1+s.p.permute(jj+1)) % 12
	a := simplexCorner(gi0, x0, y0, 0, 0.5)
	b := simplexCorner(gi1, x1, y1, 0, 0.5)
	c := simplexCorner(gi2, x2, y2, 0, 0.5)
	return float64(float32(70 * (a + b + c)))
}

func (s *SimplexNoise) Sample3D(x, y, z float64) float64 {
	x, y, z = x+s.p.ox, y+s.p.oy, z+s.p.oz
	skew := (x + y + z) * 0.3333333333333333
	i, j, k := int(math.Floor(x+skew)), int(math.Floor(y+skew)), int(math.Floor(z+skew))
	unskew := float64(i+j+k) * 0.16666666666666666
	x0, y0, z0 := x-(float64(i)-unskew), y-(float64(j)-unskew), z-(float64(k)-unskew)
	var i1, j1, k1, i2, j2, k2 int
	if x0 >= y0 {
		if y0 >= z0 {
			i1, i2, j2 = 1, 1, 1
		} else if x0 >= z0 {
			i1, i2, k2 = 1, 1, 1
		} else {
			k1, i2, k2 = 1, 1, 1
		}
	} else if y0 < z0 {
		k1, j2, k2 = 1, 1, 1
	} else if x0 < z0 {
		j1, j2, k2 = 1, 1, 1
	} else {
		j1, i2, j2 = 1, 1, 1
	}
	x1, y1, z1 := x0-float64(i1)+0.16666666666666666, y0-float64(j1)+0.16666666666666666, z0-float64(k1)+0.16666666666666666
	x2, y2, z2 := x0-float64(i2)+0.3333333333333333, y0-float64(j2)+0.3333333333333333, z0-float64(k2)+0.3333333333333333
	x3, y3, z3 := x0-1+0.5, y0-1+0.5, z0-1+0.5
	ii, jj, kk := i&255, j&255, k&255
	gi0 := s.p.permute(ii+s.p.permute(jj+s.p.permute(kk))) % 12
	gi1 := s.p.permute(ii+i1+s.p.permute(jj+j1+s.p.permute(kk+k1))) % 12
	gi2 := s.p.permute(ii+i2+s.p.permute(jj+j2+s.p.permute(kk+k2))) % 12
	gi3 := s.p.permute(ii+1+s.p.permute(jj+1+s.p.permute(kk+1))) % 12
	a := simplexCorner(gi0, x0, y0, z0, 0.6)
	b := simplexCorner(gi1, x1, y1, z1, 0.6)
	c := simplexCorner(gi2, x2, y2, z2, 0.6)
	d := simplexCorner(gi3, x3, y3, z3, 0.6)
	return float64(float32(32 * (a + b + c + d)))
}
