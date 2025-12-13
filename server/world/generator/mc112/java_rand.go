package mc112

// javaRand is a port of java.util.Random. It is used to reproduce Minecraft Java Edition 1.12 worldgen.
//
// The implementation matches the 48-bit LCG used by Java:
//
//	seed = (seed*0x5DEECE66D + 0xB) & ((1<<48)-1)
//	next(bits) = seed >> (48-bits)
type javaRand struct {
	seed uint64
}

const (
	javaRandMultiplier = uint64(0x5DEECE66D)
	javaRandAddend     = uint64(0xB)
	javaRandMask       = uint64((1 << 48) - 1)
)

func newJavaRand(seed int64) *javaRand {
	r := &javaRand{}
	r.SetSeed(seed)
	return r
}

func (r *javaRand) SetSeed(seed int64) {
	r.seed = (uint64(seed) ^ javaRandMultiplier) & javaRandMask
}

func (r *javaRand) next(bits uint) int32 {
	r.seed = (r.seed*javaRandMultiplier + javaRandAddend) & javaRandMask
	return int32(r.seed >> (48 - bits))
}

func (r *javaRand) Intn(bound int32) int32 {
	if bound <= 0 {
		panic("javaRand.Intn: bound must be positive")
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

func (r *javaRand) Float64() float64 {
	// nextDouble(): (((long)next(26) << 27) + next(27)) / (double)(1L << 53)
	hi := int64(r.next(26))
	lo := int64(r.next(27))
	return float64((hi<<27)+lo) / float64(int64(1)<<53)
}
