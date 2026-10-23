package world

// javaBiomeNames maps the Bedrock names that differ from Java to the Java
// biome each stands for. Bedrock keeps the names and the variants of biomes
// that Java 1.18 renamed or merged into others.
var javaBiomeNames = map[string]string{
	"bamboo_jungle_hills":              "bamboo_jungle",
	"birch_forest_hills":               "birch_forest",
	"birch_forest_hills_mutated":       "old_growth_birch_forest",
	"birch_forest_mutated":             "old_growth_birch_forest",
	"cold_beach":                       "snowy_beach",
	"cold_taiga":                       "snowy_taiga",
	"cold_taiga_hills":                 "snowy_taiga",
	"cold_taiga_mutated":               "snowy_taiga",
	"deep_warm_ocean":                  "warm_ocean",
	"desert_hills":                     "desert",
	"desert_mutated":                   "desert",
	"extreme_hills":                    "windswept_hills",
	"extreme_hills_edge":               "windswept_hills",
	"extreme_hills_mutated":            "windswept_gravelly_hills",
	"extreme_hills_plus_trees":         "windswept_forest",
	"extreme_hills_plus_trees_mutated": "windswept_gravelly_hills",
	"forest_hills":                     "forest",
	"hell":                             "nether_wastes",
	"ice_mountains":                    "snowy_plains",
	"ice_plains":                       "snowy_plains",
	"ice_plains_spikes":                "ice_spikes",
	"jungle_edge":                      "sparse_jungle",
	"jungle_edge_mutated":              "sparse_jungle",
	"jungle_hills":                     "jungle",
	"jungle_mutated":                   "jungle",
	"legacy_frozen_ocean":              "frozen_ocean",
	"mega_taiga":                       "old_growth_pine_taiga",
	"mega_taiga_hills":                 "old_growth_pine_taiga",
	"mesa":                             "badlands",
	"mesa_bryce":                       "eroded_badlands",
	"mesa_plateau":                     "badlands",
	"mesa_plateau_mutated":             "badlands",
	"mesa_plateau_stone":               "wooded_badlands",
	"mesa_plateau_stone_mutated":       "wooded_badlands",
	"mushroom_island":                  "mushroom_fields",
	"mushroom_island_shore":            "mushroom_fields",
	"redwood_taiga_hills_mutated":      "old_growth_spruce_taiga",
	"redwood_taiga_mutated":            "old_growth_spruce_taiga",
	"roofed_forest":                    "dark_forest",
	"roofed_forest_mutated":            "dark_forest",
	"savanna_mutated":                  "windswept_savanna",
	"savanna_plateau_mutated":          "windswept_savanna",
	"soulsand_valley":                  "soul_sand_valley",
	"stone_beach":                      "stony_shore",
	"swampland":                        "swamp",
	"swampland_mutated":                "swamp",
	"taiga_hills":                      "taiga",
	"taiga_mutated":                    "taiga",
}

// JavaBiomeName returns the Java identifier, without namespace, of the biome
// passed. Spawn rules, biome tags and loot conditions name biomes as Java does.
func JavaBiomeName(b Biome) string {
	name := b.String()
	if java, ok := javaBiomeNames[name]; ok {
		return java
	}
	return name
}
