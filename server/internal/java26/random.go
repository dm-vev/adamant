package java26

import (
	"crypto/md5"
	"encoding/binary"
	"math"
)

type State struct {
	Legacy uint64 `json:"legacy"`
	XorLo uint64 `json:"xor_lo"`
	XorHi uint64 `json:"xor_hi"`
	LegacyMode bool `json:"legacy_mode"`
	WorldgenMode bool `json:"worldgen_mode,omitempty"`
	Gaussian float64 `json:"gaussian"`
	HasGaussian bool `json:"has_gaussian"`
}
const (
	legacyMultiplier        uint64 = 25214903917
	legacyAddend            uint64 = 11
	legacyMask              uint64 = (1 << 48) - 1
	xoroshiroZeroState0     uint64 = 0x9e3779b97f4a7c15
	xoroshiroZeroState1     uint64 = 7640891576956012809
)

// Random is a mutable Java26 random stream. Copying it copies its stream state.
type Random struct {
	legacy       uint64
	xorLo, xorHi uint64
	legacyMode   bool
	worldgenMode bool
	gaussian     float64
	hasGaussian  bool
}
func (r Random) State() State {
	return State{Legacy:r.legacy, XorLo:r.xorLo, XorHi:r.xorHi, LegacyMode:r.legacyMode, WorldgenMode:r.worldgenMode, Gaussian:r.gaussian, HasGaussian:r.hasGaussian}
}

// FromState restores a Java random stream from its complete mutable state.
func FromState(state State) Random {
	r := Random{legacy: state.Legacy, xorLo: state.XorLo, xorHi: state.XorHi, legacyMode: state.LegacyMode, worldgenMode: state.WorldgenMode, gaussian: state.Gaussian, hasGaussian: state.HasGaussian}
	// Java's Xoroshiro128PlusPlus constructor replaces the all-zero state.
	if !r.legacyMode && r.xorLo|r.xorHi == 0 {
		r.xorLo, r.xorHi = xoroshiroZeroState0, xoroshiroZeroState1
	}
	return r
}

func NewLegacyRandom(seed int64) Random {
	return Random{legacy: (uint64(seed) ^ legacyMultiplier) & legacyMask, legacyMode: true}
}
func (r *Random) next(bits uint) uint32 {
	if r.legacyMode {
		r.legacy = (r.legacy*legacyMultiplier + legacyAddend) & legacyMask
		return uint32(r.legacy >> (48 - bits))
	}
	return uint32(r.xoroshiro() >> (64 - bits))
}
func (r *Random) NextInt(bound int32) int32 {
	if bound <= 0 {
		panic("java26: bound must be positive")
	}
	if r.legacyMode || r.worldgenMode {
		if bound&(bound-1) == 0 {
			return int32((int64(bound) * int64(r.next(31))) >> 31)
		}
		for {
			bits := int32(r.next(31))
			value := bits % bound
			if bits-value+(bound-1) >= 0 {
				return value
			}
		}
	}
	b := uint64(uint32(bound))
	for {
		random := uint64(uint32(r.xoroshiro()))
		product := random * b
		fraction := product & 0xffffffff
		if fraction < b {
			reject := uint64(uint32(-bound)) % b
			if fraction < reject {
				continue
			}
		}
		return int32(product >> 32)
	}
}
func (r *Random) NextInt32() int32 {
	if r.legacyMode || r.worldgenMode {
		return int32(r.next(32))
	}
	return int32(r.xoroshiro())
}
func (r *Random) NextLong() int64 {
	if r.legacyMode || r.worldgenMode {
		upper, lower := int32(r.next(32)), int32(r.next(32))
		return int64(upper)<<32 + int64(lower)
	}
	return int64(r.xoroshiro())
}
func (r *Random) NextFloat() float32 { return float32(r.next(24)) * 5.9604645e-8 }
func (r *Random) NextDouble() float64 {
	if !r.legacyMode && !r.worldgenMode {
		return float64(r.xoroshiro()>>11) * 1.1102230246251565e-16
	}
	upper, lower := r.next(26), r.next(27)
	return float64(uint64(upper)<<27+uint64(lower)) * 1.1102230246251565e-16
}

// SetSeed resets the random source using the Java XoroshiroRandomSource seed expansion.
func (r *Random) SetSeed(seed int64) {
	if r.legacyMode {
		r.legacy = (uint64(seed) ^ legacyMultiplier) & legacyMask
	} else {
		lo, hi := upgradeSeed(seed)
		r.xorLo, r.xorHi = mixStafford13(lo), mixStafford13(hi)
		if r.xorLo|r.xorHi == 0 {
			r.xorLo, r.xorHi = xoroshiroZeroState0, xoroshiroZeroState1
		}
	}
	if !r.worldgenMode {
		r.hasGaussian = false
	}
}

// SetDecorationSeed and SetFeatureSeed mirror Minecraft's WorldgenRandom methods.
func (r *Random) SetDecorationSeed(seed int64, blockX, blockZ int32) int64 {
	r.SetSeed(seed)
	xScale := r.NextLong() | 1
	zScale := r.NextLong() | 1
	decorationSeed := int64(blockX)*xScale + int64(blockZ)*zScale ^ seed
	r.SetSeed(decorationSeed)
	return decorationSeed
}

func (r *Random) SetFeatureSeed(seed int64, index, step int32) {
	r.SetSeed(seed + int64(index) + 10000*int64(step))
}

func (r *Random) NextBoolean() bool {
	if r.legacyMode || r.worldgenMode {
		return r.next(1) != 0
	}
	return r.xoroshiro()&1 != 0
}


func (r *Random) NextGaussian() float64 {
	if r.hasGaussian {
		r.hasGaussian = false
		return r.gaussian
	}
	for {
		x, y := 2*r.NextDouble()-1, 2*r.NextDouble()-1
		radius := x*x + y*y
		if radius >= 1 || radius == 0 {
			continue
		}
		multiplier := math.Sqrt(-2 * math.Log(radius) / radius)
		r.gaussian, r.hasGaussian = y*multiplier, true
		return x * multiplier
	}
}
func (r *Random) Consume(count int) {
	for range count {
		r.next(32)
	}
}
func (r *Random) ForkPositional() PositionalFactory {
	if r.legacyMode {
		return PositionalFactory{seed: r.NextLong(), legacy: true}
	}
	if r.worldgenMode {
		return PositionalFactory{seed: int64(r.xoroshiro()), seedHi: int64(r.xoroshiro())}
	}
	return PositionalFactory{seed: r.NextLong(), seedHi: r.NextLong()}
}
func (r *Random) xoroshiro() uint64 {
	s0, s1 := r.xorLo, r.xorHi
	result := bitsRotateLeft(s0+s1, 17) + s0
	s1 ^= s0
	r.xorLo = bitsRotateLeft(s0, 49) ^ s1 ^ (s1 << 21)
	r.xorHi = bitsRotateLeft(s1, 28)
	return result
}
func bitsRotateLeft(x uint64, k uint) uint64 { return x<<k | x>>(64-k) }

type PositionalFactory struct {
	seed, seedHi int64
	legacy       bool
}

func javaPositionSeed(x, y, z int32) uint64 {
	seed := int64(int32(x*3129871)) ^ int64(z)*116129781 ^ int64(y)
	seed = seed*seed*42317861 + seed*11
	return uint64(seed >> 16)
}
func (p PositionalFactory) At(x, y, z int32) Random {
	positional := javaPositionSeed(x, y, z)
	if p.legacy {
		return NewLegacyRandom(int64(positional ^ uint64(p.seed)))
	}
	return newXoroshiro(positional^uint64(p.seed), uint64(p.seedHi))
}
func (p PositionalFactory) FromHashOf(name string) Random {
	if p.legacy {
		h := int32(0)
		for _, c := range name {
			if c <= 0xffff {
				h = h*31 + int32(c)
			} else {
				value := int32(c) - 0x10000
				h = h*31 + (value >> 10) + 0xd800
				h = h*31 + (value & 0x3ff) + 0xdc00
			}
		}
		return NewLegacyRandom(int64(h) ^ p.seed)
	}
	hash := md5.Sum([]byte(name))
	lo := binary.BigEndian.Uint64(hash[:8])
	hi := binary.BigEndian.Uint64(hash[8:])
	return newXoroshiro(lo^uint64(p.seed), hi^uint64(p.seedHi))
}
func NewXoroshiroRandom(seed int64) Random {
	lo, hi := upgradeSeed(seed)
	return newXoroshiro(mixStafford13(lo), mixStafford13(hi))
}

// NewWorldgenRandom wraps a XoroshiroRandomSource with WorldgenRandom's
// next(bits) adapter, as used by Minecraft's world-generation seed streams.
func NewWorldgenRandom(seed int64) Random {
	r := NewXoroshiroRandom(seed)
	r.worldgenMode = true
	return r
}

// NewWorldgenLegacyRandom wraps a LegacyRandomSource with WorldgenRandom's
// reseeding and next(bits) semantics.
func NewWorldgenLegacyRandom(seed int64) Random {
	r := NewLegacyRandom(seed)
	r.worldgenMode = true
	return r
}
// NewRandomSequence creates the native Java random stream for a persisted
// random-sequence key, matching RandomSequences/RandomSequence.
func NewRandomSequence(seed int64, key string) Random {
	hash := md5.Sum([]byte(key))
	lo, hi := upgradeSeed(seed)
	lo ^= binary.BigEndian.Uint64(hash[:8])
	hi ^= binary.BigEndian.Uint64(hash[8:])
	return newXoroshiro(mixStafford13(lo), mixStafford13(hi))
}
func upgradeSeed(seed int64) (uint64, uint64) {
	lo := uint64(seed) ^ uint64(7640891576956012809)
	hi := lo + uint64(0x9e3779b97f4a7c15)
	return lo, hi
}
func newXoroshiro(lo, hi uint64) Random {
	if lo|hi == 0 {
		lo, hi = xoroshiroZeroState0, xoroshiroZeroState1
	}
	return Random{xorLo: lo, xorHi: hi}
}
func mixStafford13(z uint64) uint64 {
	z = (z ^ (z >> 30)) * uint64(0xbf58476d1ce4e5b9)
	z = (z ^ (z >> 27)) * uint64(0x94d049bb133111eb)
	return z ^ (z >> 31)
}
func javaFloor(v float64) int64 { return int64(math.Floor(v)) }
