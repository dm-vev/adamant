package world

// levelRandomSource follows Core::Random's MT19937 raw-word stream. Bedrock
// LevelRandom seeds it from the low 32 bits of the saved world seed. It is
// owned by the world's transaction and is not safe for concurrent use.
type levelRandomSource struct {
	state [624]uint32
	index int
}

func newLevelRandomSource(seed uint32) *levelRandomSource {
	r := &levelRandomSource{index: 624}
	r.state[0] = seed
	for i := 1; i < len(r.state); i++ {
		previous := r.state[i-1]
		r.state[i] = 1812433253*(previous^(previous>>30)) + uint32(i)
	}
	return r
}

func (r *levelRandomSource) Uint32() uint32 {
	if r.index >= len(r.state) {
		r.index = 0
	}
	// Core::Random twists one state word per draw instead of eagerly
	// twisting the full array. This also reproduces a live heap snapshot
	// whose prefix and suffix belong to different MT generations.
	i := r.index
	word := r.state[i]&0x80000000 | r.state[(i+1)%624]&0x7fffffff
	y := r.state[(i+397)%624] ^ word>>1
	if word&1 != 0 {
		y ^= 0x9908b0df
	}
	r.state[i] = y
	r.index++
	y ^= y >> 11
	y ^= y << 7 & 0x9d2c5680
	y ^= y << 15 & 0xefc60000
	return y ^ y>>18
}

// Uint64 adapts the raw stream to Go's rand.Source. Native callers needing one
// raw word must use randomUint32 rather than rand.Rand.Uint32, which obtains
// and truncates a whole Uint64. This adapter does not port every native random
// API's distribution or the order of non-redstone consumers.
func (r *levelRandomSource) Uint64() uint64 {
	hi, lo := r.Uint32(), r.Uint32()
	return uint64(hi)<<32 | uint64(lo)
}

func (r *levelRandomSource) bounded(bound uint32) uint32 {
	if bound == 0 {
		return 0
	}
	return r.Uint32() % bound
}

func (w *World) randomIntN(bound int) int {
	if native, ok := w.conf.RandSource.(*levelRandomSource); ok && bound > 0 && uint64(bound) <= 0xffffffff {
		return int(native.bounded(uint32(bound)))
	}
	return w.r.IntN(bound)
}

func (w *World) randomUint32() uint32 {
	if native, ok := w.conf.RandSource.(*levelRandomSource); ok {
		return native.Uint32()
	}
	return w.r.Uint32()
}

type worldRandomWords struct{ world *World }

func (r worldRandomWords) Uint32() uint32 { return r.world.randomUint32() }
