package block

import "github.com/df-mc/dragonfly/server/world"

// WallBlocks returns a list of all possible blocks for a wall.
func WallBlocks() []world.Block {
	return []world.Block{
		Andesite{},
		Blackstone{Type: PolishedBlackstone()},
		Blackstone{},
		Bricks{},
		Cinnabar{},
		CinnabarBricks{},
		Cobblestone{Mossy: true},
		Cobblestone{},
		DeepslateBricks{},
		DeepslateTiles{},
		Deepslate{Type: CobbledDeepslate()},
		Deepslate{Type: PolishedDeepslate()},
		Diorite{},
		EndBricks{},
		Granite{},
		MudBricks{},
		NetherBricks{Type: RedNetherBricks()},
		NetherBricks{},
		PolishedBlackstoneBrick{},
		PolishedCinnabar{},
		PolishedSulfur{},
		PolishedTuff{},
		Prismarine{},
		ResinBricks{},
		Sandstone{Red: true},
		Sandstone{},
		StoneBricks{Type: MossyStoneBricks()},
		StoneBricks{},
		Sulfur{},
		SulfurBricks{},
		Tuff{},
		TuffBricks{},
	}
}
