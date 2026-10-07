package bedrock

import (
	"math"
	"testing"
)

func TestLegacyImprovedNoiseMatchesNative(t *testing.T) {
	var random LegacyRandom
	random.Seed(5489)
	var noise LegacyImprovedNoise
	noise.Init(&random)

	values := [2]float32{}
	noise.Fill(values[:], [3]float32{-23.75, 11.5, 98.125}, [3]int{1, 2, 1}, [3]float32{1, 1, 1}, 1, [3]int{1, 1, 1})
	const wantBits = uint32(0x3e66b213)
	if bits := math.Float32bits(values[0]); bits != wantBits {
		t.Fatalf("native Perlin sample bits = %#08x, want %#08x", bits, wantBits)
	}
}
