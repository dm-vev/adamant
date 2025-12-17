package genlayer

type Layer interface {
	GetInts(x, z, w, h int) []int
}

type baseLayer struct {
	layerSalt int64
	startSalt int64
	startSeed int64
}

func newBaseLayer(salt int64) baseLayer {
	return baseLayer{layerSalt: layerSalt(salt)}
}

func (l *baseLayer) init(worldSeed int64) {
	if l.layerSalt == 0 {
		l.startSalt = 0
		l.startSeed = 0
		return
	}
	l.startSalt = startSalt(worldSeed, l.layerSalt)
	l.startSeed = stepSeed(l.startSalt, 0)
}

func (l *baseLayer) randAt(x, z int64) chunkRand {
	return chunkRand{seed: chunkSeed(l.startSeed, x, z), startSalt: l.startSalt}
}
