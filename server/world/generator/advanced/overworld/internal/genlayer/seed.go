package genlayer

const (
	mcSeedMul int64 = 6364136223846793005
	mcSeedAdd int64 = 1442695040888963407
)

func stepSeed(seed, salt int64) int64 {
	return seed*(seed*mcSeedMul+mcSeedAdd) + salt
}

func layerSalt(salt int64) int64 {
	s := stepSeed(salt, salt)
	s = stepSeed(s, salt)
	s = stepSeed(s, salt)
	return s
}

func startSalt(worldSeed, layerSalt int64) int64 {
	s := worldSeed
	s = stepSeed(s, layerSalt)
	s = stepSeed(s, layerSalt)
	s = stepSeed(s, layerSalt)
	return s
}

func startSeed(worldSeed, layerSalt int64) int64 {
	s := startSalt(worldSeed, layerSalt)
	return stepSeed(s, 0)
}

func chunkSeed(startSeed, x, z int64) int64 {
	s := startSeed + x
	s = stepSeed(s, z)
	s = stepSeed(s, x)
	s = stepSeed(s, z)
	return s
}

type chunkRand struct {
	seed      int64
	startSalt int64
}

func (r *chunkRand) nextInt(bound int) int {
	if bound <= 0 {
		panic("genlayer: nextInt bound must be positive")
	}
	v := int((r.seed >> 24) % int64(bound))
	if v < 0 {
		v += bound
	}
	r.seed = stepSeed(r.seed, r.startSalt)
	return v
}
