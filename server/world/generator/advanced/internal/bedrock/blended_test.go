package bedrock

import (
	"math"
	"testing"
)

func TestBlendedNativeVolumes(t *testing.T) {
	tests := []struct {
		name           string
		seed           int64
		chunkX, chunkZ int32
		index          int
		want           uint32
	}{
		{"cached_vertical_gradients", 0, 0, 0, 14, 0xbdc2af98},
		{"negative_chunk_upper_vertex", 1, -1, -1, 40, 0x413e400c},
		{"negative_seed", -1, 257, -257, 420, 0xc19a33ef},
		{"upper_corner_full_width_seed", -1 << 63, -1025, 513, 1024, 0x42528aca},
		{"far_cell_index_overflow", 1 << 32, -1874998, 1874998, 614, 0xc27503e0},
		{"far_positive_chunk", 0x12345678abcdef01, 1874998, -1874998, 999, 0xc01d36a2},
	}
	var values [densityGridSize]float32
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			noise := NewBlendedNoise(test.seed)
			noise.Fill(test.chunkX, test.chunkZ, &values)
			got := math.Float32bits(values[test.index])
			if got != test.want {
				t.Fatalf("blended density bits = %08x, want %08x", got, test.want)
			}
		})
	}
}
