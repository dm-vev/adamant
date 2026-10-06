package bedrock

import (
	"crypto/md5"
	"encoding/binary"
	"math/bits"
)

// RandomFactory derives independent named noise sources from a world seed.
// It is immutable and may be shared by generator workers.
type RandomFactory struct {
	low, high uint64
}

// NewRandomFactory creates the positional source used by modern Bedrock noise.
func NewRandomFactory(seed int64) RandomFactory {
	low := uint64(seed) ^ 0x6a09e667f3bcc909
	r := newRandom(mixSeed(low), mixSeed(low+0x9e3779b97f4a7c15))
	return r.fork()
}

func mixSeed(value uint64) uint64 {
	value = (value ^ (value >> 30)) * 0xbf58476d1ce4e5b9
	value = (value ^ (value >> 27)) * 0x94d049bb133111eb
	return value ^ (value >> 31)
}

func (f RandomFactory) fromName(name []byte) random {
	hash := md5.Sum(name)
	return newRandom(f.low^binary.BigEndian.Uint64(hash[:8]), f.high^binary.BigEndian.Uint64(hash[8:]))
}

// random is the xoroshiro128++ stream underlying positional noise sources.
type random struct {
	low, high uint64
}

func newRandom(low, high uint64) random {
	if low|high == 0 {
		low, high = 0x9e3779b97f4a7c15, 0x6a09e667f3bcc909
	}
	return random{low: low, high: high}
}

func (r *random) next() uint64 {
	low, high := r.low, r.high
	value := bits.RotateLeft64(low+high, 17) + low
	high ^= low
	r.low = bits.RotateLeft64(low, 49) ^ high ^ (high << 21)
	r.high = bits.RotateLeft64(high, 28)
	return value
}

func (r *random) fork() RandomFactory {
	low := r.next()
	return RandomFactory{low: low, high: r.next()}
}

func (r *random) float64() float64 {
	return float64(r.next()>>11) * 0x1p-53
}

func (r *random) bounded(bound uint32) uint32 {
	product := uint64(uint32(r.next())) * uint64(bound)
	if uint32(product) < bound {
		threshold := -bound % bound
		for uint32(product) < threshold {
			product = uint64(uint32(r.next())) * uint64(bound)
		}
	}
	return uint32(product >> 32)
}
