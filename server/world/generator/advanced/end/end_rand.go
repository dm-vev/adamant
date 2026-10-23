package end

import "github.com/df-mc/dragonfly/server/internal/vanilla"

// endRand wraps either Java's world-generation random stream for End helpers.
type endRand struct {
	random vanilla.Random
}

func newEndRand(seed int64) *endRand {
	return &endRand{random: vanilla.NewLegacyRandom(seed)}
}
func newEndRandWithRandom(random vanilla.Random) *endRand {
	return &endRand{random: random}
}

func (r *endRand) Intn(bound int32) int32 {
	return r.random.NextInt(bound)
}

func (r *endRand) Bool() bool {
	return r.random.NextBoolean()
}

func (r *endRand) islandCount() int {
	if r.random.NextFloat() >= float32(1)/14 {
		return 0
	}
	if r.random.NextInt(4) == 3 {
		return 2
	}
	return 1
}

func (r *endRand) OneIn700() bool {
	return r.random.NextFloat() < float32(1)/700
}

func (r *endRand) Long() int64 {
	return r.random.NextLong()
}


func (r *endRand) Int32() uint32 {
	return uint32(r.random.NextInt32())
}
func (g *End) endFeatureRandom(chunkX, chunkZ, featureIndex, step int) *endRand {
	random := vanilla.NewWorldgenRandom(g.seed)
	originX, originZ := int64(chunkX)*16, int64(chunkZ)*16
	decorationSeed := random.SetDecorationSeed(g.seed, int32(originX), int32(originZ))
	random.SetFeatureSeed(decorationSeed, int32(featureIndex), int32(step))
	return newEndRandWithRandom(random)
}
