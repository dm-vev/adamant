package mc112

// Rand is a port of java.util.Random. It is used to reproduce Minecraft Java Edition 1.12 worldgen.
//
// The implementation matches the 48-bit LCG used by Java:
//
//	seed = (seed*0x5DEECE66D + 0xB) & ((1<<48)-1)
//	next(bits) = seed >> (48-bits)
type Rand struct {
	seed uint64
}

const (
	randMultiplier = uint64(0x5DEECE66D)
	randAddend     = uint64(0xB)
	randMask       = uint64((1 << 48) - 1)
)

// NewRand returns a new Java-style random source initialised with seed.
func NewRand(seed int64) *Rand {
	r := &Rand{}
	r.SetSeed(seed)
	return r
}

// SetSeed sets the seed of the random source, matching java.util.Random#setSeed.
func (r *Rand) SetSeed(seed int64) {
	r.seed = (uint64(seed) ^ randMultiplier) & randMask
}

func (r *Rand) next(bits uint) int32 {
	r.seed = (r.seed*randMultiplier + randAddend) & randMask
	return int32(r.seed >> (48 - bits))
}

// Intn returns a value in [0, bound), matching java.util.Random#nextInt(bound).
func (r *Rand) Intn(bound int32) int32 {
	if bound <= 0 {
		panic("mc112.Rand.Intn: bound must be positive")
	}
	// Fast-path for power-of-two bounds.
	if bound&-bound == bound {
		return int32((int64(bound) * int64(r.next(31))) >> 31)
	}
	for {
		bits := r.next(31)
		val := bits % bound
		// This check intentionally relies on int32 overflow semantics to match Java's behaviour.
		if bits-val+(bound-1) >= 0 {
			return val
		}
	}
}

// Int32 returns a value matching java.util.Random#nextInt().
func (r *Rand) Int32() int32 {
	return r.next(32)
}

// Bool returns a value matching java.util.Random#nextBoolean().
func (r *Rand) Bool() bool {
	return r.next(1) != 0
}

// Float64 returns a float64 matching java.util.Random#nextDouble().
func (r *Rand) Float64() float64 {
	// nextDouble(): (((long)next(26) << 27) + next(27)) / (double)(1L << 53)
	hi := int64(r.next(26))
	lo := int64(r.next(27))
	return float64((hi<<27)+lo) / float64(int64(1)<<53)
}

// Float32 returns a float32 matching java.util.Random#nextFloat().
func (r *Rand) Float32() float32 {
	// nextFloat(): next(24) / (float)(1<<24)
	return float32(r.next(24)) / float32(int32(1)<<24)
}

// Long returns an int64 matching java.util.Random#nextLong().
func (r *Rand) Long() int64 {
	hi := int64(r.next(32))
	lo := int64(r.next(32))
	return (hi << 32) + lo
}
