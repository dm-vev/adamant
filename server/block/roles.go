package block

import "github.com/df-mc/dragonfly/server/world"

// IsSwitch reports whether b is a door, a trapdoor, a fence gate, a button or
// a lever: the blocks the doors-and-switches ability of Bedrock's roles
// lets a player use.
func IsSwitch(b world.Block) bool {
	switch b.(type) {
	case WoodDoor, IronDoor, CopperDoor, WoodTrapdoor, IronTrapdoor, CopperTrapdoor, WoodFenceGate, Button, Lever:
		return true
	}
	return false
}

// IsContainer reports whether b holds items, such as a chest, or opens a
// window, such as a crafting table: the blocks the open-containers ability
// of Bedrock's roles lets a player open.
func IsContainer(b world.Block) bool {
	if _, ok := b.(Container); ok {
		return true
	}
	switch b.(type) {
	case EnderChest, CraftingTable, Anvil, Beacon, EnchantingTable, Grindstone, Loom, SmithingTable, Stonecutter,
		CartographyTable, Lectern:
		return true
	}
	return false
}
