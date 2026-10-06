package bedrock

import (
	"math"
	"testing"
)

func TestBlendedNativeDensity(t *testing.T) {
	tests := []struct {
		name    string
		seed    int64
		x, y, z int32
		want    uint32
	}{
		{"positive_subcell", 0, 0, 1, 0, 0x404bb431},
		{"negative_subcell", 1, 0, -1, 0, 0xbf0d4b28},
		{"lower_boundary", -1, 0, -64, 0, 0x40b69da0},
		{"upper_boundary_full_width_seed", -1 << 63, 0, 256, 0, 0xc1b6982d},
		{"far_cell_index_overflow", 1 << 32, 9541171, 117, 28710309, 0xc19c3adf},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			noise := NewBlendedNoise(test.seed)
			got := math.Float32bits(noise.Sample(test.x, test.y, test.z))
			if got != test.want {
				t.Fatalf("blended density bits = %08x, want %08x", got, test.want)
			}
		})
	}
}
