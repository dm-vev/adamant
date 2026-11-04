package block

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// SlabBlocks returns a list of all possible blocks for a slab.
func SlabBlocks() []world.Block {
	b := []world.Block{
		Andesite{Polished: true},
		Andesite{},
		BambooMosaic{},
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
		Diorite{Polished: true},
		Diorite{},
		EndBricks{},
		Granite{Polished: true},
		Granite{},
		MudBricks{},
		NetherBricks{Type: RedNetherBricks()},
		NetherBricks{},
		PolishedBlackstoneBrick{},
		PolishedCinnabar{},
		PolishedSulfur{},
		PolishedTuff{},
		Purpur{},
		Quartz{Smooth: true},
		Quartz{},
		ResinBricks{},
		StoneBricks{Type: MossyStoneBricks()},
		StoneBricks{},
		Stone{Smooth: true},
		Stone{},
		Sulfur{},
		SulfurBricks{},
		Tuff{},
		TuffBricks{},
	}
	for _, p := range PrismarineTypes() {
		b = append(b, Prismarine{Type: p})
	}
	for _, s := range SandstoneTypes() {
		if s != ChiseledSandstone() {
			b = append(b, Sandstone{Type: s})
			b = append(b, Sandstone{Type: s, Red: true})
		}
	}
	for _, w := range WoodTypes() {
		b = append(b, Planks{Wood: w})
	}
	for _, c := range item.Colours() {
		b = append(b, Wool{Colour: c}, Concrete{Colour: c})
	}
	b = append(b, PetrifiedOak{})
	for _, o := range OxidationTypes() {
		b = append(b, Copper{Type: CutCopper(), Oxidation: o})
		b = append(b, Copper{Type: CutCopper(), Oxidation: o, Waxed: true})
	}
	return b
}
