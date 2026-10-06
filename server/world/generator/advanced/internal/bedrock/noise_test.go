package bedrock

import (
	"math"
	"testing"
)

// Expected bits were captured by executing the BDS 1.26.52.3 NormalNoise
// sampler on seeded octave stacks. These cover gapped octaves, signed seeds,
// and the float32 precision loss at large coordinates.
func TestNormalNoiseNativePrecision(t *testing.T) {
	cases := []struct {
		name       string
		seed       int64
		noise      string
		octave     int
		amplitudes []float32
		position   [3]float32
		bits       uint32
	}{
		{"gapped_octaves", 0, "minecraft:temperature", -10, []float32{1.5, 0, 1, 0, 0, 0}, [3]float32{3161966, -741944, -2383429}, 0xbbd164d8},
		{"negative_seed", -1, "minecraft:continentalness", -9, []float32{1, 1, 2, 2, 2, 1, 1, 1, 1}, [3]float32{653113, 7453338, 3879325}, 0xbe112aaf},
		{"high_seed_bits", 1 << 32, "minecraft:offset", -3, []float32{1, 1, 1, 0}, [3]float32{-5013538, 7239588, 2690029}, 0xbec17124},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			noise := NewRandomFactory(test.seed).Noise(test.noise, test.octave, test.amplitudes...)
			got := math.Float32bits(noise.Sample(test.position[0], test.position[1], test.position[2]))
			if got != test.bits {
				t.Fatalf("native noise bits: got %08x, want %08x", got, test.bits)
			}
		})
	}
}
