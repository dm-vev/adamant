package structure

import (
	"fmt"
	"strings"
)

// StructureTagMembers resolves the Java structure tags used by the vanilla
// worldgen registry. A bare structure identifier is returned as a singleton.
func StructureTagMembers(raw string) ([]string, error) {
	isTag := strings.HasPrefix(raw, "#")
	name := strings.TrimPrefix(raw, "#")
	if name == "" {
		return nil, fmt.Errorf("empty structure selector")
	}
	if !strings.Contains(name, ":") {
		name = "minecraft:" + name
	}
	if !isTag {
		return []string{name}, nil
	}
	if !strings.HasPrefix(name, "minecraft:") {
		return nil, fmt.Errorf("unknown structure tag %q", name)
	}
	members, ok := structureTags[strings.TrimPrefix(name, "minecraft:")]
	if !ok {
		return nil, fmt.Errorf("unknown structure tag %q", name)
	}
	return members, nil
}

var structureTags = map[string][]string{
	"village": {"minecraft:village_desert", "minecraft:village_plains", "minecraft:village_savanna", "minecraft:village_snowy", "minecraft:village_taiga"},
	"mineshaft": {"minecraft:mineshaft", "minecraft:mineshaft_mesa"},
	"shipwreck": {"minecraft:shipwreck"},
	"ruined_portal": {"minecraft:ruined_portal", "minecraft:ruined_portal_desert", "minecraft:ruined_portal_jungle", "minecraft:ruined_portal_mountain", "minecraft:ruined_portal_nether", "minecraft:ruined_portal_ocean", "minecraft:ruined_portal_swamp"},
	"ocean_ruin": {"minecraft:ocean_ruin_cold", "minecraft:ocean_ruin_warm"},
	"abandoned_camp": {"minecraft:abandoned_camp_bamboo_jungle", "minecraft:abandoned_camp_birch_forest", "minecraft:abandoned_camp_cherry_grove", "minecraft:abandoned_camp_dappled_forest", "minecraft:abandoned_camp_flower_forest", "minecraft:abandoned_camp_forest", "minecraft:abandoned_camp_meadow", "minecraft:abandoned_camp_old_growth_birch_forest", "minecraft:abandoned_camp_old_growth_pine_taiga", "minecraft:abandoned_camp_old_growth_spruce_taiga", "minecraft:abandoned_camp_pale_garden", "minecraft:abandoned_camp_savanna", "minecraft:abandoned_camp_sparse_jungle", "minecraft:abandoned_camp_snowy_taiga", "minecraft:abandoned_camp_swamp", "minecraft:abandoned_camp_taiga", "minecraft:abandoned_camp_windswept_forest", "minecraft:abandoned_camp_wooded_badlands"},
	"on_abandoned_camp_bamboo_jungle": {"minecraft:abandoned_camp_bamboo_jungle"},
	"on_abandoned_camp_cherry_grove": {"minecraft:abandoned_camp_cherry_grove"},
	"on_abandoned_camp_birch_forest": {"minecraft:abandoned_camp_birch_forest"},
	"on_abandoned_camp_dappled_forest": {"minecraft:abandoned_camp_dappled_forest"},
	"on_abandoned_camp_flower_forest": {"minecraft:abandoned_camp_flower_forest"},
	"on_abandoned_camp_pale_garden": {"minecraft:abandoned_camp_pale_garden"},
	"on_abandoned_camp_swamp": {"minecraft:abandoned_camp_swamp"},
	"on_abandoned_camp_windswept_forest": {"minecraft:abandoned_camp_windswept_forest"},
	"on_woodland_mansion_maps": {"minecraft:mansion"},
	"on_ocean_monument_maps": {"minecraft:monument"},
	"on_savanna_village_maps": {"minecraft:village_savanna"},
	"on_desert_village_maps": {"minecraft:village_desert"},
	"on_plains_village_maps": {"minecraft:village_plains"},
	"on_taiga_village_maps": {"minecraft:village_taiga"},
	"on_snowy_village_maps": {"minecraft:village_snowy"},
	"on_jungle_pyramid_maps": {"minecraft:jungle_pyramid"},
	"on_swamp_hut_maps": {"minecraft:swamp_hut"},
	"on_treasure_maps": {"minecraft:buried_treasure"},
	"on_buried_trial_chambers_maps": {"minecraft:trial_chambers"},
	"on_ancient_city_maps": {"minecraft:ancient_city"},
	"on_mineshaft_maps": {"minecraft:mineshaft", "minecraft:mineshaft_mesa"},
	"on_desert_pyramid_maps": {"minecraft:desert_pyramid"},
	"on_ocean_ruin_warm_maps": {"minecraft:ocean_ruin_warm"},
	"eye_of_ender_located": {"minecraft:stronghold"},
	"dolphin_located": {"minecraft:shipwreck", "minecraft:ocean_ruin_cold", "minecraft:ocean_ruin_warm"},
	"cats_spawn_in": {"minecraft:village_desert", "minecraft:village_plains", "minecraft:village_savanna", "minecraft:village_snowy", "minecraft:village_taiga", "minecraft:swamp_hut"},
	"cats_spawn_as_black": {"minecraft:swamp_hut"},
}
