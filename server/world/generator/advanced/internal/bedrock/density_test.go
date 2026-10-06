package bedrock

import (
	"math"
	"testing"
)

func TestTerrainNativeGrid(t *testing.T) {
	// Captured from BDS 1.26.52.3's complete density composer, including the
	// native bulk Perlin history and auxiliary-noise density cutoff.
	tests := []struct {
		name           string
		seed           int64
		chunkX, chunkZ int32
		index          int
		want           [densityFields]uint32
	}{
		{"active_auxiliaries", -1, 257, -257, 20, [densityFields]uint32{0xc24b7038, 0x3e653ed3, 0x3e960bbf, 0x3f0386ce, 0xbed2e591, 0x3f27ab5e, 0xbec026a2, 0x3e404be5}},
		{"reused_grid_skipped_auxiliaries", 0, 0, 0, 20, [densityFields]uint32{0xc34e9718, 0x3f800000, 0x3f800000, 0x3f800000, 0x3f800000, 0x3f800000, 0x3f800000, 0x3f800000}},
		{"lower_slide", 0, 0, 0, 0, [densityFields]uint32{0x41700000, 0xbe2fb2f2, 0xbf2e6e23, 0xb99b24ab, 0xbd39c3fe, 0xbe567631, 0x3e58a328, 0xbe7fe1fb}},
		{"cached_blended_column", 0, 0, 0, 14, [densityFields]uint32{0x42bfc912, 0xbe9b3026, 0xbe1216b3, 0x3c8954cc, 0xbed9f6ab, 0x3dc8f011, 0xbe2b4665, 0x3e62ae79}},
		{"upper_slide", 1, -1, -1, 40, [densityFields]uint32{0xc1200000, 0xbf1a3783, 0xbec86164, 0x3f424a22, 0x3ec7ca86, 0xbd8a0f2f, 0x3df50746, 0x3df6bd36}},
		{"full_width_seed_upper_corner", -1 << 63, -1025, 513, 1024, [densityFields]uint32{0xc1200000, 0x3df82bda, 0x3e2f1802, 0xbd85d022, 0x3ee8154c, 0x3e5ea95f, 0x3e0fdabf, 0xbef5c04b}},
		{"far_coordinates", 0x12345678abcdef01, 1874998, -1874998, 999, [densityFields]uint32{0x4206abae, 0xbd9b26f9, 0xbe913e4e, 0xbe89fc87, 0x3eca8109, 0xbc8def00, 0x3f3b96ae, 0x3d2acd40}},
	}
	var grid TerrainGrid
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			climate := NewClimate(test.seed)
			density := NewTerrainDensity(test.seed, NewTerrainShape(test.seed, climate))
			density.Prepare(test.chunkX, test.chunkZ, &grid)
			for field, want := range test.want {
				if got := math.Float32bits(grid.values[field][test.index]); got != want {
					t.Errorf("field %d bits = %08x, want %08x", field, got, want)
				}
			}
		})
	}
}

func TestTerrainCellInterpolation(t *testing.T) {
	// Scalar Z/X/Y evaluation of captured native vertices. These values cover
	// non-vertex samples and do not claim execution of BDS's full voxel loop.
	tests := []struct {
		seed           int64
		chunkX, chunkZ int32
		x, z           uint8
		y              int16
		want           [4]uint32
	}{
		{0, 0, 0, 1, 14, -61, [4]uint32{0x3d1e8fb5, 0xbe47d084, 0xbc9646bc, 0x3e68233e}},
		{1, -1, -1, 15, 0, 255, [4]uint32{0xbe1b134f, 0x3db28f70, 0xbd521280, 0x3d71ef60}},
		{-1, 257, -257, 8, 7, 0, [4]uint32{0x3db96468, 0xbe0e07bd, 0xbe959360, 0xbdedfc2c}},
		{0x12345678abcdef01, 1874998, -1874998, 9, 6, 7, [4]uint32{0x3d96d0cd, 0xbe312936, 0x3e5e0c35, 0x3ce2dae8}},
	}
	var grid TerrainGrid
	var cell TerrainCell
	for _, test := range tests {
		climate := NewClimate(test.seed)
		NewTerrainDensity(test.seed, NewTerrainShape(test.seed, climate)).Prepare(test.chunkX, test.chunkZ, &grid)
		grid.PrepareCell(test.x>>2, uint8((test.y+64)>>3), test.z>>2, &cell)
		cell.SetZ(test.z & 3)
		cell.SetX(test.x & 3)
		y := uint8(test.y & 7)
		a, b, c := cell.VeinNoise(y)
		got := [4]uint32{math.Float32bits(cell.Density(y)), math.Float32bits(a), math.Float32bits(b), math.Float32bits(c)}
		if got != test.want {
			t.Errorf("seed %d chunk (%d,%d) voxel (%d,%d,%d): bits %08x, want %08x", test.seed, test.chunkX, test.chunkZ, test.x, test.y, test.z, got, test.want)
		}
	}
}

func TestTerrainCellNoodleBoundary(t *testing.T) {
	var grid TerrainGrid
	for i := range grid.values[densityTerrain] {
		grid.values[densityTerrain][i] = 128
	}
	var cell TerrainCell
	grid.PrepareCell(0, 0, 0, &cell)
	cell.SetZ(3)
	cell.SetX(1)
	if got := math.Float32bits(cell.Density(3)); got != 0x3e9e3f62 {
		t.Fatalf("noodle cave changed density below Y=-60: %08x", got)
	}
	if got := math.Float32bits(cell.Density(4)); got != 0xbd99999a {
		t.Fatalf("zero noodle selector did not carve at Y=-60: %08x", got)
	}
	for i := range grid.values[densityNoodle] {
		grid.values[densityNoodle][i] = -0.25
	}
	cell.SetZ(3)
	cell.SetX(1)
	if got := math.Float32bits(cell.Density(4)); got != 0x3e9e3f62 {
		t.Fatalf("negative noodle selector carved solid density: %08x", got)
	}
}
