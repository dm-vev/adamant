package bedrock

import (
	"math"
	"testing"
)

func TestTerrainNativeColumns(t *testing.T) {
	tests := []struct {
		name                   string
		seed                   int64
		x, z, height           int32
		offset, factor, jagged uint32
	}{
		{"positive_jaggedness", 0, 6466, -11703, 144, 0x3e3a5544, 0x4449999a, 0x36dc0d16},
		{"negative_jaggedness", 0, -4417, 16704, 120, 0x3c6daac0, 0x4449999a, 0xba4425ab},
		{"native_far_coordinate_overflow", 0, -18109242, -23046225, 80, 0xbe7f67de, 0x43c631bb, 0xffc00000},
		{"low_surface_full_width_seed", 1<<63 - 1, -27270335, -22664363, 0, 0xbf057517, 0x42a00000, 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			shape := NewTerrainShape(test.seed, NewClimate(test.seed))
			var column TerrainColumn
			shape.SampleColumn(test.x, test.z, &column)
			actual := [3]uint32{math.Float32bits(column.Offset), math.Float32bits(column.Factor), math.Float32bits(column.Jaggedness)}
			if expected := [3]uint32{test.offset, test.factor, test.jagged}; actual != expected {
				t.Fatalf("terrain column bits = %08x, want %08x", actual, expected)
			}
			if height := shape.SurfaceHeight(test.x, test.z); height != test.height {
				t.Fatalf("preliminary surface = %d, want %d", height, test.height)
			}
		})
	}
}
