package gamedata

import "testing"

func TestBlockIsSolidUsesRegistryResults(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"stone", true},
		{"minecraft:air", false},
		{"minecraft:water", false},
		{"minecraft:oak_leaves", true},
		{"minecraft:oak_button", false},
		{"minecraft:amethyst_cluster", true},
		{"minecraft:bamboo", true},
		{"minecraft:scaffolding", false},
	}
	for _, tc := range cases {
		got, err := BlockIsSolid(tc.name)
		if err != nil {
			t.Errorf("BlockIsSolid(%q): %v", tc.name, err)
			continue
		}
		if got != tc.want {
			t.Errorf("BlockIsSolid(%q) = %t, want %t", tc.name, got, tc.want)
		}
	}
}

func TestBlockIsSolidRejectsUnknownBlocks(t *testing.T) {
	for _, name := range []string{"adamant:stone", "minecraft:not_a_block", ""} {
		if got, err := BlockIsSolid(name); err == nil {
			t.Errorf("BlockIsSolid(%q) = %t, want an error", name, got)
		}
	}
}
