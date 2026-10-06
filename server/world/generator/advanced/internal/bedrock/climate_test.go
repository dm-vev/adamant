package bedrock

import "testing"

// Biomes were observed through the BDS 1.26.52.3 getBiome API. The sampler
// returns Java resource names, so the observed swampland is named swamp here.
func TestClimateObservedBiomes(t *testing.T) {
	type observation struct {
		x, y, z int32
		biome   string
	}
	cases := []struct {
		seed   int64
		points []observation
	}{
		{0, []observation{{-48, -64, 0, "minecraft:forest"}}},
		{1, []observation{{-16, -60, -16, "minecraft:deep_ocean"}}},
		{1 << 32, []observation{{-16, -60, -16, "minecraft:taiga"}}},
		{-1, []observation{
			{-16392, -16, 8220, "minecraft:swamp"},
			{-16392, 0, 8220, "minecraft:sulfur_caves"},
			{-16392, 16, 8220, "minecraft:swamp"},
		}},
		{9223372036854775807, []observation{
			{-16, -60, -16, "minecraft:deep_dark"},
			{4120, -32, -4100, "minecraft:river"},
			{4120, -16, -4100, "minecraft:dappled_forest"},
		}},
	}
	for _, test := range cases {
		climate := NewClimate(test.seed)
		var column ClimateColumn
		for _, point := range test.points {
			x, y, z := BiomeCell(uint32(test.seed), point.x, point.y, point.z)
			climate.SampleColumn(x*8, z*8, &column)
			var lookup ClimateLookup
			var got string
			// Native generation starts its climate column one cell below
			// the Overworld and retains the previous winner while ascending.
			for height := int32(-72); height <= y*8; height += 8 {
				got = lookup.Biome(column.PointAt(height))
			}
			if got != point.biome {
				t.Fatalf("seed %d at (%d,%d,%d): got %s, want %s", test.seed, point.x, point.y, point.z, got, point.biome)
			}
		}
	}
}

// These boundary outcomes were captured from the native BDS tree search.
// Equality retains the previous leaf, independently for each climate column.
func TestClimateLookupTieHistory(t *testing.T) {
	var left, right ClimateLookup
	point := ClimatePoint{0, 0, -10500, 0, 0, 0}
	if got := left.Biome(&point); got != "minecraft:mushroom_fields" {
		t.Fatalf("left neighbour: got %s", got)
	}
	point[2] = -10498
	if got := right.Biome(&point); got != "minecraft:deep_ocean" {
		t.Fatalf("right neighbour: got %s", got)
	}
	point[2] = -10499
	if got := left.Biome(&point); got != "minecraft:mushroom_fields" {
		t.Fatalf("left history lost at boundary: got %s", got)
	}
	if got := right.Biome(&point); got != "minecraft:deep_ocean" {
		t.Fatalf("right history lost at boundary: got %s", got)
	}
	right = ClimateLookup{}
	if got := right.Biome(&point); got != "minecraft:mushroom_fields" {
		t.Fatalf("reset did not restore native first-leaf ordering: got %s", got)
	}
}
