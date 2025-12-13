package mc112

import "math"

// noiseImproved is a port of net.minecraft.world.gen.NoiseGeneratorImproved.
type noiseImproved struct {
	permutations [512]int
	xCoord       float64
	yCoord       float64
	zCoord       float64
}

var (
	gradX  = [16]float64{1, -1, 1, -1, 1, -1, 1, -1, 0, 0, 0, 0, 1, 0, -1, 0}
	gradY  = [16]float64{1, 1, -1, -1, 0, 0, 0, 0, 1, -1, 1, -1, 1, -1, 1, -1}
	gradZ  = [16]float64{0, 0, 0, 0, 1, 1, -1, -1, 1, 1, -1, -1, 0, 1, 0, -1}
	grad2X = [16]float64{1, -1, 1, -1, 1, -1, 1, -1, 0, 0, 0, 0, 1, 0, -1, 0}
	grad2Z = [16]float64{0, 0, 0, 0, 1, 1, -1, -1, 1, 1, -1, -1, 0, 1, 0, -1}
)

func newNoiseImproved(r *javaRand) *noiseImproved {
	n := &noiseImproved{
		xCoord: r.Float64() * 256.0,
		yCoord: r.Float64() * 256.0,
		zCoord: r.Float64() * 256.0,
	}
	for i := 0; i < 256; i++ {
		n.permutations[i] = i
	}
	for l := 0; l < 256; l++ {
		j := int(r.Intn(int32(256-l))) + l
		k := n.permutations[l]
		n.permutations[l] = n.permutations[j]
		n.permutations[j] = k
		n.permutations[l+256] = n.permutations[l]
	}
	return n
}

func (n *noiseImproved) lerp(t, a, b float64) float64 {
	return a + t*(b-a)
}

func (n *noiseImproved) grad2(hash int, x, z float64) float64 {
	i := hash & 15
	return grad2X[i]*x + grad2Z[i]*z
}

func (n *noiseImproved) grad(hash int, x, y, z float64) float64 {
	i := hash & 15
	return gradX[i]*x + gradY[i]*y + gradZ[i]*z
}

func fade(t float64) float64 {
	return t * t * t * (t*(t*6.0-15.0) + 10.0)
}

// populateNoiseArray adds noise into noiseArray. The behaviour matches the Minecraft 1.12 implementation.
func (n *noiseImproved) populateNoiseArray(noiseArray []float64, xOffset, yOffset, zOffset float64, xSize, ySize, zSize int, xScale, yScale, zScale, noiseScale float64) {
	if ySize == 1 {
		i5 := 0
		j5 := 0
		j := 0
		k5 := 0
		d14 := 0.0
		d15 := 0.0
		l5 := 0
		d16 := 1.0 / noiseScale

		for j2 := 0; j2 < xSize; j2++ {
			d17 := xOffset + float64(j2)*xScale + n.xCoord
			i6 := int(d17)
			if d17 < float64(i6) {
				i6--
			}
			k2 := i6 & 255
			d17 -= float64(i6)
			d18 := fade(d17)

			for j6 := 0; j6 < zSize; j6++ {
				d19 := zOffset + float64(j6)*zScale + n.zCoord
				k6 := int(d19)
				if d19 < float64(k6) {
					k6--
				}
				l6 := k6 & 255
				d19 -= float64(k6)
				d20 := fade(d19)

				i5 = n.permutations[k2]
				j5 = n.permutations[i5] + l6
				j = n.permutations[k2+1]
				k5 = n.permutations[j] + l6

				d14 = n.lerp(d18,
					n.grad2(n.permutations[j5], d17, d19),
					n.grad(n.permutations[k5], d17-1.0, 0.0, d19),
				)
				d15 = n.lerp(d18,
					n.grad(n.permutations[j5+1], d17, 0.0, d19-1.0),
					n.grad(n.permutations[k5+1], d17-1.0, 0.0, d19-1.0),
				)

				d21 := n.lerp(d20, d14, d15)
				noiseArray[l5] += d21 * d16
				l5++
			}
		}
		return
	}

	i := 0
	d0 := 1.0 / noiseScale
	k := -1
	l := 0
	i1 := 0
	j1 := 0
	k1 := 0
	l1 := 0
	i2 := 0
	d1 := 0.0
	d2 := 0.0
	d3 := 0.0
	d4 := 0.0

	for l2 := 0; l2 < xSize; l2++ {
		d5 := xOffset + float64(l2)*xScale + n.xCoord
		i3 := int(d5)
		if d5 < float64(i3) {
			i3--
		}
		j3 := i3 & 255
		d5 -= float64(i3)
		d6 := fade(d5)

		for k3 := 0; k3 < zSize; k3++ {
			d7 := zOffset + float64(k3)*zScale + n.zCoord
			l3 := int(d7)
			if d7 < float64(l3) {
				l3--
			}
			i4 := l3 & 255
			d7 -= float64(l3)
			d8 := fade(d7)

			for j4 := 0; j4 < ySize; j4++ {
				d9 := yOffset + float64(j4)*yScale + n.yCoord
				k4 := int(d9)
				if d9 < float64(k4) {
					k4--
				}
				l4 := k4 & 255
				d9 -= float64(k4)
				d10 := fade(d9)

				if j4 == 0 || l4 != k {
					k = l4
					l = n.permutations[j3] + l4
					i1 = n.permutations[l] + i4
					j1 = n.permutations[l+1] + i4
					k1 = n.permutations[j3+1] + l4
					l1 = n.permutations[k1] + i4
					i2 = n.permutations[k1+1] + i4

					d1 = n.lerp(d6,
						n.grad(n.permutations[i1], d5, d9, d7),
						n.grad(n.permutations[l1], d5-1.0, d9, d7),
					)
					d2 = n.lerp(d6,
						n.grad(n.permutations[j1], d5, d9-1.0, d7),
						n.grad(n.permutations[i2], d5-1.0, d9-1.0, d7),
					)
					d3 = n.lerp(d6,
						n.grad(n.permutations[i1+1], d5, d9, d7-1.0),
						n.grad(n.permutations[l1+1], d5-1.0, d9, d7-1.0),
					)
					d4 = n.lerp(d6,
						n.grad(n.permutations[j1+1], d5, d9-1.0, d7-1.0),
						n.grad(n.permutations[i2+1], d5-1.0, d9-1.0, d7-1.0),
					)
				}

				d11 := n.lerp(d10, d1, d2)
				d12 := n.lerp(d10, d3, d4)
				d13 := n.lerp(d8, d11, d12)
				noiseArray[i] += d13 * d0
				i++
			}
		}
	}
}

// clamp is an internal helper mirroring MathHelper.clamp for floats.
func clamp(v, min, max float64) float64 {
	return math.Max(min, math.Min(max, v))
}
