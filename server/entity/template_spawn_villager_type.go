package entity

// finalizeTemplateVillagerType includes the concrete authored-load precedence:
// Villager and ZombieVillager mark decoded VillagerData finalized even when
// the saved flag is false or absent. Only an unauthored type follows the biome.
func finalizeTemplateVillagerType(nbt map[string]any, biome string) {
	data, _ := nbt["VillagerData"].(map[string]any)
	if data != nil {
		nbt["VillagerDataFinalized"] = byte(1)
		return
	}
	if templateSpawnBool(nbt["VillagerDataFinalized"]) {
		return
	}
	if data == nil {
		data = map[string]any{"type": "minecraft:plains", "profession": "minecraft:none", "level": int32(1)}
		nbt["VillagerData"] = data
	}
	data["type"] = templateVillagerType(biome)
	nbt["VillagerDataFinalized"] = byte(1)
}

func templateVillagerType(biome string) string {
	switch biome {
	case "minecraft:badlands", "minecraft:desert", "minecraft:eroded_badlands", "minecraft:wooded_badlands":
		return "minecraft:desert"
	case "minecraft:bamboo_jungle", "minecraft:jungle", "minecraft:sparse_jungle":
		return "minecraft:jungle"
	case "minecraft:savanna_plateau", "minecraft:savanna", "minecraft:windswept_savanna":
		return "minecraft:savanna"
	case "minecraft:deep_frozen_ocean", "minecraft:frozen_ocean", "minecraft:frozen_river", "minecraft:ice_spikes", "minecraft:snowy_beach", "minecraft:snowy_taiga", "minecraft:snowy_plains", "minecraft:grove", "minecraft:snowy_slopes", "minecraft:frozen_peaks", "minecraft:jagged_peaks":
		return "minecraft:snow"
	case "minecraft:swamp", "minecraft:mangrove_swamp":
		return "minecraft:swamp"
	case "minecraft:old_growth_spruce_taiga", "minecraft:old_growth_pine_taiga", "minecraft:windswept_gravelly_hills", "minecraft:windswept_hills", "minecraft:taiga", "minecraft:windswept_forest":
		return "minecraft:taiga"
	default:
		return "minecraft:plains"
	}
}

func templateSpawnBool(value any) bool {
	switch value := value.(type) {
	case byte:
		return value != 0
	case int8:
		return value != 0
	case bool:
		return value
	default:
		return false
	}
}
