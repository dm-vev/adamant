package potion

import "strings"

// javaIDs are the Java potion identifiers indexed by Bedrock potion ID. Java
// has no long mundane potion; it keeps Bedrock's name.
var javaIDs = [...]string{
	"water", "mundane", "long_mundane", "thick", "awkward", "night_vision", "long_night_vision", "invisibility",
	"long_invisibility", "leaping", "long_leaping", "strong_leaping", "fire_resistance", "long_fire_resistance",
	"swiftness", "long_swiftness", "strong_swiftness", "slowness", "long_slowness", "water_breathing",
	"long_water_breathing", "healing", "strong_healing", "harming", "strong_harming", "poison", "long_poison",
	"strong_poison", "regeneration", "long_regeneration", "strong_regeneration", "strength", "long_strength",
	"strong_strength", "weakness", "long_weakness", "wither", "turtle_master", "long_turtle_master",
	"strong_turtle_master", "slow_falling", "long_slow_falling", "strong_slowness",
}

// FromJavaID returns the potion with the Java identifier passed, with or
// without the minecraft namespace.
func FromJavaID(id string) (Potion, bool) {
	id = strings.TrimPrefix(id, "minecraft:")
	for i, name := range javaIDs {
		if name == id {
			return Potion{potion(i)}, true
		}
	}
	return Potion{}, false
}

// JavaID returns the namespaced Java identifier of the potion.
func (p Potion) JavaID() string {
	if i := int(p.Uint8()); i < len(javaIDs) {
		return "minecraft:" + javaIDs[i]
	}
	return "minecraft:water"
}
