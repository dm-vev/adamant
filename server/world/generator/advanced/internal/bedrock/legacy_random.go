package bedrock

// LegacyRandom is the native 32-bit MT19937 stream used by Bedrock's legacy
// octave stacks and Nether/End generation.
type LegacyRandom struct {
	state [624]uint32
	index int
}

// Seed resets the stream using the native MT19937 seed recurrence.
func (r *LegacyRandom) Seed(seed uint32) {
	r.state[0] = seed
	for i := 1; i < len(r.state); i++ {
		previous := r.state[i-1]
		r.state[i] = 0x6c078965*(previous^(previous>>30)) + uint32(i)
	}
	r.index = len(r.state)
}

// NextUint32 returns the next tempered MT19937 word.
func (r *LegacyRandom) NextUint32() uint32 {
	if r.index >= len(r.state) {
		for i := range r.state {
			next := (r.state[i] & 0x80000000) | (r.state[(i+1)%len(r.state)] & 0x7fffffff)
			value := r.state[(i+397)%len(r.state)] ^ (next >> 1)
			if next&1 != 0 {
				value ^= 0x9908b0df
			}
			r.state[i] = value
		}
		r.index = 0
	}

	value := r.state[r.index]
	r.index++
	value ^= value >> 11
	value ^= (value & 0x013a58ad) << 7
	value ^= (value & 0x0001df8c) << 15
	return value ^ (value >> 18)
}

// NextInt returns a bounded value using native modulo reduction. Bound must
// be positive.
func (r *LegacyRandom) NextInt(bound uint32) uint32 {
	return r.NextUint32() % bound
}

// NextLong concatenates two consecutive MT19937 words, high word first.
func (r *LegacyRandom) NextLong() int64 {
	return int64(uint64(r.NextUint32())<<32 | uint64(r.NextUint32()))
}

// NextFloat converts one random word to the native float32 result.
func (r *LegacyRandom) NextFloat() float32 {
	return float32(float64(r.NextUint32()) * 0x1p-32)
}

// NextDouble converts one random word to the native [0,1) double range.
func (r *LegacyRandom) NextDouble() float64 {
	return float64(r.NextUint32()) * 0x1p-32
}
