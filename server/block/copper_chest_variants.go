package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

var (
	hashCopperChest = NextHash()
	hashVault = NextHash()
	hashTrialSpawner = NextHash()
	hashChiseledBookshelf = NextHash()
	hashCrafter = NextHash()
)
func allCopperChests() []world.Block {
	blocks := make([]world.Block, 0, 32)
	for _, oxidation := range OxidationTypes() {
		for _, waxed := range []bool{false, true} {
			for _, facing := range []cube.Direction{cube.North, cube.East, cube.South, cube.West} {
				chest := NewCopperChest()
				chest.Oxidation, chest.Waxed, chest.Facing = oxidation, waxed, facing
				blocks = append(blocks, chest)
			}
		}
	}
	return blocks
}

func allCopperChestItems() []world.Item {
	items := make([]world.Item, 0, 8)
	for _, oxidation := range OxidationTypes() {
		for _, waxed := range []bool{false, true} {
			chest := NewCopperChest()
			chest.Oxidation, chest.Waxed = oxidation, waxed
			items = append(items, chest)
		}
	}
	return items
}

func allVaults() []world.Block {
	blocks := make([]world.Block, 0, 32)
	for state := VaultInactive; state <= VaultEjecting; state++ {
		for _, ominous := range []bool{false, true} {
			for _, facing := range []cube.Direction{cube.North, cube.East, cube.South, cube.West} {
				vault := NewVault()
				vault.State, vault.Ominous, vault.Facing = state, ominous, facing
				blocks = append(blocks, vault)
			}
		}
	}
	return blocks
}

func allTrialSpawners() []world.Block {
	blocks := make([]world.Block, 0, 12)
	for state := TrialSpawnerInactive; state <= TrialSpawnerCooldown; state++ {
		for _, ominous := range []bool{false, true} {
			spawner := NewTrialSpawner()
			spawner.State, spawner.Ominous = state, ominous
			blocks = append(blocks, spawner)
		}
	}
	return blocks
}
func allChiseledBookshelves() []world.Block {
	blocks := make([]world.Block, 0, 256)
	for _, facing := range []cube.Direction{cube.North, cube.East, cube.South, cube.West} {
		for mask := uint8(0); mask < 64; mask++ {
			bookshelf := NewChiseledBookshelf().WithStoredMask(mask)
			bookshelf.Facing = facing
			blocks = append(blocks, bookshelf)
		}
	}
	return blocks
}

func (c CopperChest) Hash() (uint64, uint64) {
	return hashCopperChest, uint64(c.Oxidation.Uint8()) | uint64(boolByte(c.Waxed))<<2 | uint64(c.Facing)<<3
}
