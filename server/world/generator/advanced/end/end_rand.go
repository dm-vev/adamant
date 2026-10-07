package end

import "github.com/df-mc/dragonfly/server/internal/java26"

// endRand wraps either Java's world-generation random stream for End helpers.
type endRand struct {
	random java26.Random
}

func newEndRand(seed int64) *endRand {
	return &endRand{random: java26.NewLegacyRandom(seed)}
}
func newEndRandWithRandom(random java26.Random) *endRand {
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
	random := java26.NewXoroshiroRandom(g.seed)
	xScale, zScale := random.NextLong()|1, random.NextLong()|1
	originX, originZ := int64(chunkX)*16, int64(chunkZ)*16
	decorationSeed := originX*xScale + originZ*zScale ^ g.seed
	featureSeed := decorationSeed + int64(featureIndex) + 10000*int64(step)
	return newEndRandWithRandom(java26.NewXoroshiroRandom(featureSeed))
}
