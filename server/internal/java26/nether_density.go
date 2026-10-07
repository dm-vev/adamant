package java26

import "math"

// NetherDensity is the Java 26.3 Nether router's terrain density sampler. It
// implements NoiseRouterData.nether, slideNetherLike and postProcess; cave
// carving and structure beard density are separate generation stages.
type NetherDensity struct {
	noise *BlendedNoise
}

// NewNetherDensity builds BASE_3D_NOISE_NETHER from RandomState's legacy
// positional "terrain" stream (NoiseRouterData.java and RandomState.java).
func NewNetherDensity(seed int64) *NetherDensity {
	random := NewLegacyRandom(seed)
	terrain := random.ForkPositional().FromHashOf("minecraft:terrain")
	return &NetherDensity{noise: NewBlendedNoise(&terrain, 0.25, 0.375, 80, 60, 8)}
}

// Sample returns the post-processed and squeezed Nether density at an
// interpolation lattice point. Java's Nether router uses a 4x8x4 lattice.
func (n *NetherDensity) Sample(x, y, z int32) float32 {
	value := n.noise.Sample(x, y, z)
	// NoiseRouterData.slideNetherLike: top (104..128) tends to .9375,
	// bottom (-8..16) tends to 2.5. Nether minY is zero.
	top := clampNether(float32(y-104)/24, 0, 1)
	value = float32(float32(1-top)*0.9375 + top*value)
	bottom := clampNether(float32(y+8)/24, 0, 1)
	value = float32(float32(1-bottom)*2.5 + bottom*value)
	value = float32(value * 0.64)
	value = clampNether(value, -1, 1)
	return float32(value/2 - value*value*value/24)
}

// Fill writes the vanilla 5x17x5 density vertices for a chunk. Values are in
// x/y/z order with y contiguous, matching the terrain interpolation loops.
func (n *NetherDensity) Fill(chunkX, chunkZ int32, values *[425]float32) {
	for x := range 5 {
		for z := range 5 {
			for y := range 17 {
				values[(x*5+z)*17+y] = n.Sample((chunkX*4)+int32(x*4), int32(y*8), (chunkZ*4)+int32(z*4))
			}
		}
	}
}

func clampNether(value, low, high float32) float32 {
	return float32(math.Max(float64(low), math.Min(float64(high), float64(value))))
}
