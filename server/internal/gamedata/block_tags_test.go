package gamedata

import "testing"

func TestMotionBlockingHeightmapTags(t *testing.T) {
	cases := []struct {
		tag   string
		block string
		want  bool
	}{
		{"minecraft:blocks_motion_in_heightmap", "minecraft:stone", true},
		{"minecraft:blocks_motion_in_heightmap", "minecraft:oak_leaves", true},
		{"minecraft:blocks_motion_in_heightmap_no_leaves", "minecraft:oak_leaves", false},
		{"minecraft:blocks_motion_in_heightmap_no_leaves", "stone", true},
		{"minecraft:blocks_motion_in_heightmap", "minecraft:water", false},
	}
	for _, tc := range cases {
		got, err := BlockTagContains(tc.tag, tc.block)
		if err != nil {
			t.Errorf("BlockTagContains(%q, %q): %v", tc.tag, tc.block, err)
			continue
		}
		if got != tc.want {
			t.Errorf("BlockTagContains(%q, %q) = %t, want %t", tc.tag, tc.block, got, tc.want)
		}
	}
}

func TestBiomeTagsExpandNestedReferences(t *testing.T) {
	cases := []struct {
		biome string
		want  bool
	}{
		{"minecraft:deep_frozen_ocean", true},
		{"minecraft:plains", false},
	}
	for _, tc := range cases {
		got, err := BiomeTagContains("minecraft:has_structure/ocean_monument", tc.biome)
		if err != nil {
			t.Errorf("BiomeTagContains(%q): %v", tc.biome, err)
			continue
		}
		if got != tc.want {
			t.Errorf("BiomeTagContains(%q) = %t, want %t", tc.biome, got, tc.want)
		}
	}
}
