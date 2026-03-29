package item_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

func TestBoatEncodeItemByVariant(t *testing.T) {
	tests := []struct {
		variant item.BoatVariant
		name    string
	}{
		{variant: item.BoatVariantOak, name: "minecraft:oak_boat"},
		{variant: item.BoatVariantSpruce, name: "minecraft:spruce_boat"},
		{variant: item.BoatVariantBirch, name: "minecraft:birch_boat"},
		{variant: item.BoatVariantJungle, name: "minecraft:jungle_boat"},
		{variant: item.BoatVariantAcacia, name: "minecraft:acacia_boat"},
		{variant: item.BoatVariantDarkOak, name: "minecraft:dark_oak_boat"},
		{variant: item.BoatVariantMangrove, name: "minecraft:mangrove_boat"},
		{variant: item.BoatVariantBamboo, name: "minecraft:bamboo_raft"},
		{variant: item.BoatVariantCherry, name: "minecraft:cherry_boat"},
		{variant: item.BoatVariantPaleOak, name: "minecraft:pale_oak_boat"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			boat := item.Boat{Variant: tt.variant}
			if got, _ := boat.EncodeItem(); got != tt.name {
				t.Fatalf("Boat{Variant:%d}.EncodeItem() = %q, want %q", tt.variant, got, tt.name)
			}
			if _, _, ok := world.ItemRuntimeID(boat); !ok {
				t.Fatalf("runtime ID missing for %q", tt.name)
			}
		})
	}
}

func TestChestBoatEncodeItemByVariant(t *testing.T) {
	tests := []struct {
		variant item.BoatVariant
		name    string
	}{
		{variant: item.BoatVariantOak, name: "minecraft:oak_chest_boat"},
		{variant: item.BoatVariantSpruce, name: "minecraft:spruce_chest_boat"},
		{variant: item.BoatVariantBirch, name: "minecraft:birch_chest_boat"},
		{variant: item.BoatVariantJungle, name: "minecraft:jungle_chest_boat"},
		{variant: item.BoatVariantAcacia, name: "minecraft:acacia_chest_boat"},
		{variant: item.BoatVariantDarkOak, name: "minecraft:dark_oak_chest_boat"},
		{variant: item.BoatVariantMangrove, name: "minecraft:mangrove_chest_boat"},
		{variant: item.BoatVariantBamboo, name: "minecraft:bamboo_chest_raft"},
		{variant: item.BoatVariantCherry, name: "minecraft:cherry_chest_boat"},
		{variant: item.BoatVariantPaleOak, name: "minecraft:pale_oak_chest_boat"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			boat := item.ChestBoat{Variant: tt.variant}
			if got, _ := boat.EncodeItem(); got != tt.name {
				t.Fatalf("ChestBoat{Variant:%d}.EncodeItem() = %q, want %q", tt.variant, got, tt.name)
			}
			if _, _, ok := world.ItemRuntimeID(boat); !ok {
				t.Fatalf("runtime ID missing for %q", tt.name)
			}
		})
	}
}

func TestBoatVariantFromInt(t *testing.T) {
	if got := item.BoatVariantFromInt(-1); got != item.BoatVariantOak {
		t.Fatalf("BoatVariantFromInt(-1) = %d, want %d", got, item.BoatVariantOak)
	}
	if got := item.BoatVariantFromInt(999); got != item.BoatVariantOak {
		t.Fatalf("BoatVariantFromInt(999) = %d, want %d", got, item.BoatVariantOak)
	}
	if got := item.BoatVariantFromInt(int(item.BoatVariantBamboo)); got != item.BoatVariantBamboo {
		t.Fatalf("BoatVariantFromInt(%d) = %d, want %d", item.BoatVariantBamboo, got, item.BoatVariantBamboo)
	}
}
