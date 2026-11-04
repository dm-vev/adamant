package block

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// shapeNames are the names the slab, double slab, stairs and wall of a material take, without the namespace,
// for every material that one of them is made of. A shape that has no block of a material has no name for it.
type shapeNames struct {
	slab, doubleSlab string
	// stairs and wall are the names without their suffix, "_stairs" and "_wall".
	stairs, wall string
}

// materialShapeNames holds the shapeNames of every material of SlabBlocks, StairsBlocks and WallBlocks.
var materialShapeNames = func() map[world.Block]shapeNames {
	m := map[world.Block]shapeNames{}
	// The names of the slab without "_slab", of the stairs and of the wall: the slab is "<name>_slab" and the
	// double slab "<name>_double_slab".
	for b, n := range map[world.Block][3]string{
		Andesite{Polished: true}:               {"polished_andesite", "polished_andesite", ""},
		Andesite{}:                             {"andesite", "andesite", "andesite"},
		BambooMosaic{}:                         {"bamboo_mosaic", "bamboo_mosaic", ""},
		Blackstone{Type: PolishedBlackstone()}: {"polished_blackstone", "polished_blackstone", "polished_blackstone"},
		Blackstone{}:                           {"blackstone", "blackstone", "blackstone"},
		Bricks{}:                               {"brick", "brick", "brick"},
		Cinnabar{}:                             {"cinnabar", "cinnabar", "cinnabar"},
		CinnabarBricks{}:                       {"cinnabar_brick", "cinnabar_brick", "cinnabar_brick"},
		Cobblestone{Mossy: true}:               {"mossy_cobblestone", "mossy_cobblestone", "mossy_cobblestone"},
		Cobblestone{}:                          {"cobblestone", "stone", "cobblestone"},
		DeepslateBricks{}:                      {"deepslate_brick", "deepslate_brick", "deepslate_brick"},
		DeepslateTiles{}:                       {"deepslate_tile", "deepslate_tile", "deepslate_tile"},
		Deepslate{Type: CobbledDeepslate()}:    {"cobbled_deepslate", "cobbled_deepslate", "cobbled_deepslate"},
		Deepslate{Type: PolishedDeepslate()}:   {"polished_deepslate", "polished_deepslate", "polished_deepslate"},
		Diorite{Polished: true}:                {"polished_diorite", "polished_diorite", ""},
		Diorite{}:                              {"diorite", "diorite", "diorite"},
		EndBricks{}:                            {"end_stone_brick", "end_brick", "end_stone_brick"},
		Granite{Polished: true}:                {"polished_granite", "polished_granite", ""},
		Granite{}:                              {"granite", "granite", "granite"},
		MudBricks{}:                            {"mud_brick", "mud_brick", "mud_brick"},
		NetherBricks{Type: RedNetherBricks()}:  {"red_nether_brick", "red_nether_brick", "red_nether_brick"},
		NetherBricks{}:                         {"nether_brick", "nether_brick", "nether_brick"},
		PolishedBlackstoneBrick{}:              {"polished_blackstone_brick", "polished_blackstone_brick", "polished_blackstone_brick"},
		PolishedCinnabar{}:                     {"polished_cinnabar", "polished_cinnabar", "polished_cinnabar"},
		PolishedSulfur{}:                       {"polished_sulfur", "polished_sulfur", "polished_sulfur"},
		PolishedTuff{}:                         {"polished_tuff", "polished_tuff", "polished_tuff"},
		Purpur{}:                               {"purpur", "purpur", ""},
		Quartz{Smooth: true}:                   {"smooth_quartz", "smooth_quartz", ""},
		Quartz{}:                               {"quartz", "quartz", ""},
		ResinBricks{}:                          {"resin_brick", "resin_brick", "resin_brick"},
		StoneBricks{Type: MossyStoneBricks()}:  {"mossy_stone_brick", "mossy_stone_brick", "mossy_stone_brick"},
		StoneBricks{}:                          {"stone_brick", "stone_brick", "stone_brick"},
		Stone{Smooth: true}:                    {"smooth_stone", "", ""},
		Stone{}:                                {"normal_stone", "normal_stone", ""},
		Sulfur{}:                               {"sulfur", "sulfur", "sulfur"},
		SulfurBricks{}:                         {"sulfur_brick", "sulfur_brick", "sulfur_brick"},
		Tuff{}:                                 {"tuff", "tuff", "tuff"},
		TuffBricks{}:                           {"tuff_brick", "tuff_brick", "tuff_brick"},
		Prismarine{Type: NormalPrismarine()}:   {"prismarine", "prismarine", "prismarine"},
		Prismarine{Type: DarkPrismarine()}:     {"dark_prismarine", "dark_prismarine", ""},
		Prismarine{Type: BrickPrismarine()}:    {"prismarine_brick", "prismarine_bricks", ""},
		Sandstone{Type: NormalSandstone()}:     {"sandstone", "sandstone", "sandstone"},
		Sandstone{Type: NormalSandstone(), Red: true}: {"red_sandstone", "red_sandstone", "red_sandstone"},
		Sandstone{Type: CutSandstone()}:               {"cut_sandstone", "", ""},
		Sandstone{Type: CutSandstone(), Red: true}:    {"cut_red_sandstone", "", ""},
		Sandstone{Type: SmoothSandstone()}:            {"smooth_sandstone", "smooth_sandstone", ""},
		Sandstone{Type: SmoothSandstone(), Red: true}: {"smooth_red_sandstone", "smooth_red_sandstone", ""},
		PetrifiedOak{}: {"petrified_oak", "", ""},
	} {
		m[b] = shapeNames{slab: n[0] + "_slab", doubleSlab: n[0] + "_double_slab", stairs: n[1], wall: n[2]}
	}
	for _, w := range WoodTypes() {
		m[Planks{Wood: w}] = shapeNames{slab: w.String() + "_slab", doubleSlab: w.String() + "_double_slab", stairs: w.String()}
	}
	for _, c := range item.Colours() {
		for _, b := range []world.Block{Wool{Colour: c}, Concrete{Colour: c}} {
			name := c.String() + "_wool"
			if _, concrete := b.(Concrete); concrete {
				name = c.String() + "_concrete"
			}
			m[b] = shapeNames{slab: name + "_slab", doubleSlab: name + "_double_slab", stairs: name}
		}
	}
	// Cut copper takes its oxidation and waxing before its name, and its double slab is "double_cut_copper_slab".
	for _, o := range OxidationTypes() {
		for _, waxed := range []bool{false, true} {
			var prefix string
			if o != UnoxidisedOxidation() {
				prefix = o.String() + "_"
			}
			if waxed {
				prefix = "waxed_" + prefix
			}
			m[Copper{Type: CutCopper(), Oxidation: o, Waxed: waxed}] = shapeNames{
				slab: prefix + "cut_copper_slab", doubleSlab: prefix + "double_cut_copper_slab", stairs: prefix + "cut_copper",
			}
		}
	}
	return m
}()

// slabName returns the name of the slab, or of the double slab, of a material without the namespace. It panics
// if there is no slab of the material.
func slabName(block world.Block, double bool) string {
	n := materialShapeNames[block]
	name := n.slab
	if double {
		name = n.doubleSlab
	}
	if name == "" {
		panic("invalid block used for slab")
	}
	return name
}

// encodeStairsBlock returns the name of the stairs of a material without the namespace and "_stairs". It panics
// if there are no stairs of the material.
func encodeStairsBlock(block world.Block) string {
	if name := materialShapeNames[block].stairs; name != "" {
		return name
	}
	panic("invalid block used for stairs")
}

// encodeWallBlock returns the name of the wall of a material without the namespace and "_wall". It panics if
// there is no wall of the material.
func encodeWallBlock(block world.Block) string {
	if name := materialShapeNames[block].wall; name != "" {
		return name
	}
	panic("invalid block used for wall")
}
