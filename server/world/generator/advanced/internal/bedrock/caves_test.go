package bedrock

import (
	"math"
	"testing"
)

func TestCaveNativeDensity(t *testing.T) {
	tests := []struct {
		name          string
		seed          int64
		x, y, z       int32
		density, want uint32
	}{
		{"surface_side_of_selector", 0, 257, -10, -257, 0x4347ffbe, 0x4347ffbe},
		{"underground_side_of_selector", 0, 257, -10, -257, 0x43480000, 0x4215ac9d},
		{"cheese_transition_endpoint", 0, 257, -10, -257, 0x4348c800, 0x40a2771e},
		{"native_unordered_far_density", 0, -27670127, 262, -27069840, 0x7fc00000, 0x420d66d4},
		{"pillar_rounding_full_width_seed", -1 << 63, 7846, 86, 8925, 0x43c80000, 0x41048746},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			caves := NewCaveDensity(test.seed)
			got := math.Float32bits(caves.Sample(test.x, test.y, test.z, math.Float32frombits(test.density)))
			if got != test.want {
				t.Fatalf("cave density bits = %08x, want %08x", got, test.want)
			}
		})
	}
}
