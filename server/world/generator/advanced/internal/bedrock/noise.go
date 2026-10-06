package bedrock

import (
	"crypto/md5"
	"encoding/binary"
	"math"
	"strconv"
)

// NormalNoise combines two independently seeded octave stacks. Its immutable
// sampling state is safe to share between concurrent chunk generators.
type NormalNoise struct {
	first, second []improvedNoise
	amplitude     float32
}

// Noise constructs a named normal-noise source. Zero amplitudes still occupy
// octave positions, but do not allocate or evaluate unused Perlin samplers.
func (f RandomFactory) Noise(name string, firstOctave int, amplitudes ...float32) NormalNoise {
	count, first, last := 0, len(amplitudes), -1
	for i, amplitude := range amplitudes {
		if amplitude != 0 {
			count++
			if i < first {
				first = i
			}
			last = i
		}
	}
	if count == 0 {
		return NormalNoise{}
	}

	storage := make([]improvedNoise, count*2)
	noise := NormalNoise{first: storage[:count], second: storage[count:]}
	deviation := float32(0.1) * (1 + 1/float32(last-first+1))
	noise.amplitude = float32(1.0/6.0) / deviation

	r := f.fromName([]byte(name))
	firstFactory, secondFactory := r.fork(), r.fork()
	denominator := float32(math.Ldexp(1, len(amplitudes)) - 1)
	var key [32]byte
	copy(key[:], "octave_")
	next := 0
	for i, amplitude := range amplitudes {
		if amplitude == 0 {
			continue
		}
		index := firstOctave + i
		hash := md5.Sum(strconv.AppendInt(key[:7], int64(index), 10))
		low, high := binary.BigEndian.Uint64(hash[:8]), binary.BigEndian.Uint64(hash[8:])
		a := newRandom(firstFactory.low^low, firstFactory.high^high)
		b := newRandom(secondFactory.low^low, secondFactory.high^high)
		frequency := float32(math.Ldexp(1, index))
		weight := float32(math.Ldexp(1, len(amplitudes)-i-1)) / denominator
		weight = float32(weight * amplitude)
		noise.first[next].init(&a, frequency, weight)
		noise.second[next].init(&b, frequency, weight)
		next++
	}
	return noise
}

// Sample evaluates the source at noise-space coordinates. Explicit float32
// rounding prevents fused operations from changing Bedrock results on CPUs
// where the compiler can emit fused multiply-add instructions.
func (n *NormalNoise) Sample(x, y, z float32) float32 {
	var first, second float32
	for i := range n.first {
		p := &n.first[i]
		first += float32(p.sample(x, y, z) * p.amplitude)
	}
	const secondScale = float32(337.0 / 331.0)
	x, y, z = float32(x*secondScale), float32(y*secondScale), float32(z*secondScale)
	for i := range n.second {
		p := &n.second[i]
		second += float32(p.sample(x, y, z) * p.amplitude)
	}
	return float32((first + second) * n.amplitude)
}

type improvedNoise struct {
	permutation [256]uint8
	x, y, z     float32
	frequency   float32
	amplitude   float32
}

func (n *improvedNoise) init(r *random, frequency, amplitude float32) {
	n.x, n.y, n.z = float32(r.float64()*256), float32(r.float64()*256), float32(r.float64()*256)
	n.frequency, n.amplitude = frequency, amplitude
	for i := range n.permutation {
		n.permutation[i] = uint8(i)
	}
	for i := range n.permutation {
		j := i + int(r.bounded(uint32(256-i)))
		n.permutation[i], n.permutation[j] = n.permutation[j], n.permutation[i]
	}
}

func (n *improvedNoise) sample(x, y, z float32) float32 {
	x = float32(x*n.frequency) + n.x
	y = float32(y*n.frequency) + n.y
	z = float32(z*n.frequency) + n.z
	ix, iy, iz := floor32(x), floor32(y), floor32(z)
	x, y, z = x-float32(ix), y-float32(iy), z-float32(iz)
	a, b := n.permutation[uint8(ix)], n.permutation[uint8(ix+1)]
	aa, ab := n.permutation[a+uint8(iy)], n.permutation[a+uint8(iy)+1]
	ba, bb := n.permutation[b+uint8(iy)], n.permutation[b+uint8(iy)+1]
	sx, sy, sz := fade32(x), fade32(y), fade32(z)
	low := lerp32(sy,
		lerp32(sx, gradient(n.permutation[aa+uint8(iz)], x, y, z), gradient(n.permutation[ba+uint8(iz)], x-1, y, z)),
		lerp32(sx, gradient(n.permutation[ab+uint8(iz)], x, y-1, z), gradient(n.permutation[bb+uint8(iz)], x-1, y-1, z)),
	)
	high := lerp32(sy,
		lerp32(sx, gradient(n.permutation[aa+uint8(iz)+1], x, y, z-1), gradient(n.permutation[ba+uint8(iz)+1], x-1, y, z-1)),
		lerp32(sx, gradient(n.permutation[ab+uint8(iz)+1], x, y-1, z-1), gradient(n.permutation[bb+uint8(iz)+1], x-1, y-1, z-1)),
	)
	return lerp32(sz, low, high)
}

func floor32(value float32) int32 {
	i := int32(value)
	if value < float32(i) {
		return i - 1
	}
	return i
}

func fade32(value float32) float32 {
	cube := float32(float32(value*value) * value)
	polynomial := float32(float32(float32(value*6)-15)*value) + 10
	return float32(cube * polynomial)
}

func lerp32(t, a, b float32) float32 {
	return float32((b-a)*t) + a
}

var gradients = [16][3]int8{
	{1, 1, 0}, {-1, 1, 0}, {1, -1, 0}, {-1, -1, 0},
	{1, 0, 1}, {-1, 0, 1}, {1, 0, -1}, {-1, 0, -1},
	{0, 1, 1}, {0, -1, 1}, {0, 1, -1}, {0, -1, -1},
	{1, 1, 0}, {0, -1, 1}, {-1, 1, 0}, {0, -1, -1},
}

func gradient(hash uint8, x, y, z float32) float32 {
	g := &gradients[hash&15]
	xy := float32(float32(g[0])*x) + float32(float32(g[1])*y)
	return xy + float32(float32(g[2])*z)
}
