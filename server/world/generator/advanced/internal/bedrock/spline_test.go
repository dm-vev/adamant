package bedrock

import (
	"math"
	"testing"
)

// The MCP 26.3 spline shapes produce these bits in both Java and the native
// BDS 1.26.52.3 spline evaluator. The factor case guards shattered-terrain
// branches that differ from the ordinary hills branch.
func TestTerrainSplineBoundaries(t *testing.T) {
	cases := []struct {
		name    string
		sample  func(float32, float32, float32) float32
		c, e, w float32
		bits    uint32
	}{
		{"offset_knot", TerrainOffset, 0.25, -0.7, 0.8, 0x3f34ec22},
		{"shattered_factor", TerrainFactor, -0.11, 0.05, -0.8, 0x40c954b4},
		{"jaggedness_knot", TerrainJaggedness, 0.65, -0.78, 0.56666666, 0x3e04b653},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := math.Float32bits(test.sample(test.c, test.e, test.w)); got != test.bits {
				t.Fatalf("spline bits: got %08x, want %08x", got, test.bits)
			}
		})
	}
}
