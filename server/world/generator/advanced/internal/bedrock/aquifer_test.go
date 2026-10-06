package bedrock

import (
	"math"
	"testing"
)

func TestAquiferNativeTransitionsAndChunkReuse(t *testing.T) {
	// Native BDS queries with prescribed preliminary surfaces exercise the
	// fluid rules independently of later terrain and surface generation.
	tests := []struct {
		name                    string
		seed                    int64
		x, y, z, surface, level int32
		lava, update            bool
		pressure                uint32
	}{
		{"lava_boundary", 0, 2, -55, 0, 48, -54, true, true, 0},
		{"above_lava_boundary", 0, 3, -54, 0, 48, -4096, false, false, 0},
		{"separating_barrier", 0, 10, 32, 0, 48, 17, false, true, 0x40bd70a4},
		{"local_lava_negative_coordinates", 0, -29999963, -40, 29999968, 48, -20, true, false, 0},
		{"full_width_seed", 1 << 32, 6, -32, 0, 64, -26, false, false, 0},
		{"negative_chunk_barrier", 1<<63 - 1, -8, 154, -9, 180, 137, false, true, 0x3fd1eb86},
	}
	var scratch AquiferChunk
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			aquifer := NewAquifer(test.seed, NewClimate(test.seed))
			aquifer.Prepare(test.x>>4, test.z>>4, func(_, _ int32) int32 { return test.surface }, &scratch)
			actual := scratch.Sample(test.x, test.y, test.z)
			if actual.Level != test.level || actual.Lava != test.lava || actual.ScheduleFluidUpdate != test.update || math.Float32bits(actual.Pressure) != test.pressure {
				t.Fatalf("sample = %+v (pressure bits %08x), want level=%d lava=%t update=%t pressure=%08x", actual, math.Float32bits(actual.Pressure), test.level, test.lava, test.update, test.pressure)
			}
		})
	}
}
