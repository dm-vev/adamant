package world

import "testing"

// TestJavaEntityTypeIDs holds the Java entity type of every Bedrock
// identifier whose Java type differs, and of some that do not, to the
// table loot predicates matched by when it was in the loot package.
func TestJavaEntityTypeIDs(t *testing.T) {
	for id, want := range map[string]string{
		"minecraft:zombie_pigman":          "minecraft:zombified_piglin",
		"minecraft:ender_crystal":          "minecraft:end_crystal",
		"minecraft:fireworks_rocket":       "minecraft:firework_rocket",
		"minecraft:thrown_trident":         "minecraft:trident",
		"minecraft:evocation_illager":      "minecraft:evoker",
		"minecraft:vindication_illager":    "minecraft:vindicator",
		"minecraft:wither_skull_dangerous": "minecraft:wither_skull",
		"minecraft:wind_charge_projectile": "minecraft:wind_charge",
		"minecraft:breeze_wind_charge":     "minecraft:breeze_wind_charge",
		"minecraft:snow_golem":             "minecraft:snow_golem",
		"minecraft:villager_v2":            "minecraft:villager",
		"minecraft:zombie_villager_v2":     "minecraft:zombie_villager",
		"minecraft:leash_knot":             "minecraft:leash_knot",
		"minecraft:xp_orb":                 "minecraft:experience_orb",
		"minecraft:xp_bottle":              "minecraft:experience_bottle",
		"minecraft:splash_potion":          "minecraft:splash_potion",
		"minecraft:ender_pearl":            "minecraft:ender_pearl",
		"minecraft:eye_of_ender_signal":    "minecraft:eye_of_ender",
		"minecraft:fishing_hook":           "minecraft:fishing_bobber",
		// Types named alike in both editions keep their name.
		"minecraft:zombie": "minecraft:zombie",
		"minecraft:pig":    "minecraft:pig",
	} {
		if got := JavaEntityTypeID(id); got != want {
			t.Errorf("JavaEntityTypeID(%q) = %q, want %q", id, got, want)
		}
	}
	if len(bedrockToJavaEntity) != 19 {
		t.Errorf("bedrockToJavaEntity has %d entries, want 19", len(bedrockToJavaEntity))
	}
}
