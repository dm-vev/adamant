package loot

import "strings"

// bedrockTablePaths maps the Java loot tables that BDS names differently to
// their path below loot_tables, without the .json extension. The other
// tables a container saves are named alike in both editions.
var bedrockTablePaths = map[string]string{
	"archaeology/desert_pyramid":     "entities/desert_pyramid_brushable_block",
	"archaeology/desert_well":        "entities/desert_well_brushable_block",
	"archaeology/ocean_ruin_cold":    "entities/cold_ocean_ruins_brushable_block",
	"archaeology/ocean_ruin_warm":    "entities/warm_ocean_ruins_brushable_block",
	"archaeology/trail_ruins_common": "entities/trail_ruins_brushable_block_common",
	"archaeology/trail_ruins_rare":   "entities/trail_ruins_brushable_block_rare",
	"chests/buried_treasure":         "chests/buriedtreasure",
	"chests/jungle_temple_dispenser": "chests/dispenser_trap",
	"chests/shipwreck_map":           "chests/shipwreck",
	"chests/shipwreck_supply":        "chests/shipwrecksupply",
	"chests/shipwreck_treasure":      "chests/shipwrecktreasure",
}

// javaTableKeys is the inverse of bedrockTablePaths.
var javaTableKeys = func() map[string]string {
	m := make(map[string]string, len(bedrockTablePaths))
	for java, bedrock := range bedrockTablePaths {
		m[bedrock] = java
	}
	return m
}()

// BedrockTablePath returns the path BDS saves the Java loot table with the key
// passed by, as in the LootTable of a container, such as
// loot_tables/chests/buriedtreasure.json for minecraft:chests/buried_treasure.
func BedrockTablePath(key string) string {
	if key == "" {
		return ""
	}
	name := strings.TrimPrefix(key, "minecraft:")
	if path, ok := bedrockTablePaths[name]; ok {
		name = path
	}
	return "loot_tables/" + name + ".json"
}

// JavaTableKey returns the key of the Java loot table that a BDS path such as
// loot_tables/chests/buriedtreasure.json names. A Java key is returned as it
// is, with the minecraft namespace if it had none.
func JavaTableKey(name string) string {
	path, ok := strings.CutPrefix(name, "loot_tables/")
	if !ok {
		if name != "" && !strings.Contains(name, ":") {
			return "minecraft:" + name
		}
		return name
	}
	path = strings.TrimSuffix(path, ".json")
	if java, ok := javaTableKeys[path]; ok {
		path = java
	}
	return "minecraft:" + path
}
