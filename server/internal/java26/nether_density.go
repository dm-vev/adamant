package java26

import "math"

// NetherDensity is the Java 26.3 Nether router's terrain density sampler. It
// implements NoiseRouterData.nether, slideNetherLike and postProcess; cave
// carving and structure beard density are separate generation stages.
type NetherDensity struct {
	noise *BlendedNoise
}

// NewNetherDensity builds BASE_3D_NOISE_NETHER from RandomState's direct legacy
// seed stream. Legacy terrain bypasses the positional "terrain" hash.
func NewNetherDensity(seed int64) *NetherDensity {
	random := NewLegacyRandom(seed)
	return &NetherDensity{noise: NewBlendedNoise(&random, 0.25, 0.375, 80, 60, 8)}
}

// Sample returns the scaled Nether density at a 4x8x4 interpolation vertex.
// Squeeze follows interpolation, not lattice sampling.
func (n *NetherDensity) Sample(x, y, z int32) float32 {
	value := float32(0.9375)
	if y < 128 {
		value = n.noise.Sample(x, y, z)
		if y > 104 {
			top := float32(1 - float32(y-104)/24)
			value = noiseLerp(top, 0.9375, value)
		}
	}
	// The bottom gradient spans -8..24, rather than a 24-block interval.
	bottom := clampNether(float32(y+8)/32, 0, 1)
	if bottom == 0 {
		value = 2.5
	} else if bottom != 1 {
		value = noiseLerp(bottom, 2.5, value)
	}
	return float32(value * 0.64)
}

// Fill writes the vanilla 5x17x5 density vertices for a chunk. Values are in
// x/z/y order with y contiguous, matching the terrain interpolation loops.
func (n *NetherDensity) Fill(chunkX, chunkZ int32, values *[425]float32) {
	for x := range 5 {
		for z := range 5 {
			for y := range 17 {
				values[(x*5+z)*17+y] = n.Sample(chunkX*16+int32(x*4), int32(y*8), chunkZ*16+int32(z*4))
			}
		}
	}
}

// Column writes final Nether densities for one raw 0..127 noise column.
// As in InterpolatedFunction.sampleVolume, Z and X are interpolated first,
// followed by incremental Y steps and then squeeze.
func (n *NetherDensity) Column(x, z int32, values *[128]float32) {
	cellX, cellZ := x&^3, z&^3
	fx, fz := float32(x&3)*0.25, float32(z&3)*0.25
	var lower float32
	for vertexY := range 17 {
		y := int32(vertexY * 8)
		upper := n.Sample(cellX, y, cellZ)
		if fz != 0 {
			upper = noiseLerp(fz, upper, n.Sample(cellX, y, cellZ+4))
		}
		if fx != 0 {
			right := n.Sample(cellX+4, y, cellZ)
			if fz != 0 {
				right = noiseLerp(fz, right, n.Sample(cellX+4, y, cellZ+4))
			}
			upper = noiseLerp(fx, upper, right)
		}
		if vertexY != 0 {
			step := float32((upper - lower) * 0.125)
			density := lower
			for dy := range 8 {
				value := clampNether(density, -1, 1)
				values[(vertexY-1)*8+dy] = float32(value/2 - float32(float32(value*value)*value)/24)
				density = float32(density + step)
			}
		}
		lower = upper
	}
}

func clampNether(value, low, high float32) float32 {
	return float32(math.Max(float64(low), math.Min(float64(high), float64(value))))
}
