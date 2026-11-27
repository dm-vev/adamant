package loot

import "testing"

func TestBedrockTablePaths(t *testing.T) {
	for tableKey, bedrock := range map[string]string{
		"minecraft:chests/simple_dungeon":             "loot_tables/chests/simple_dungeon.json",
		"minecraft:chests/buried_treasure":            "loot_tables/chests/buriedtreasure.json",
		"minecraft:chests/shipwreck_supply":           "loot_tables/chests/shipwrecksupply.json",
		"minecraft:chests/jungle_temple_dispenser":    "loot_tables/chests/dispenser_trap.json",
		"minecraft:archaeology/ocean_ruin_cold":       "loot_tables/entities/cold_ocean_ruins_brushable_block.json",
		"minecraft:dispensers/trial_chambers/chamber": "loot_tables/dispensers/trial_chambers/chamber.json",
	} {
		if got := BedrockTablePath(tableKey); got != bedrock {
			t.Errorf("BedrockTablePath(%s) = %s, want %s", tableKey, got, bedrock)
		}
		if got := TableKey(bedrock); got != tableKey {
			t.Errorf("JavaTableKey(%s) = %s, want %s", bedrock, got, tableKey)
		}
		if got := TableKey(tableKey); got != tableKey {
			t.Errorf("JavaTableKey(%s) = %s, want it unchanged", tableKey, got)
		}
	}
	// Every renamed table is a loot table Adamant has.
	for tableKey := range bedrockTablePaths {
		if _, ok := tables["minecraft:"+tableKey]; !ok {
			t.Errorf("renamed table %s is unknown", tableKey)
		}
	}
}
