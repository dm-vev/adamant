package world

// javaEntityNames maps the Java identifiers of the entity types registered
// by their Bedrock identifiers to those, so that Java NBT and spawners find
// them.
var javaEntityNames = map[string]string{
	"minecraft:zombified_piglin": "minecraft:zombie_pigman",
	"minecraft:end_crystal":      "minecraft:ender_crystal",
	"minecraft:experience_orb":   "minecraft:xp_orb",
	"minecraft:eye_of_ender":     "minecraft:eye_of_ender_signal",
	"minecraft:firework_rocket":  "minecraft:fireworks_rocket",
	"minecraft:wind_charge":      "minecraft:wind_charge_projectile",
}

// bedrockEntityIdentifiers maps the names entity types are registered and
// compared by to the Bedrock identifiers BDS saves and clients spawn, for the
// types whose names differ: clients show nothing for minecraft:evoker or
// minecraft:tropical_fish and the legacy, pre-1.14 mob for minecraft:villager.
var bedrockEntityIdentifiers = map[string]string{
	"minecraft:villager":           "minecraft:villager_v2",
	"minecraft:zombie_villager":    "minecraft:zombie_villager_v2",
	"minecraft:evoker":             "minecraft:evocation_illager",
	"minecraft:tropical_fish":      "minecraft:tropicalfish",
	"minecraft:evoker_fangs":       "minecraft:evocation_fang",
	"minecraft:breeze_wind_charge": "minecraft:breeze_wind_charge_projectile",
}

// registeredEntityNames is the inverse of bedrockEntityIdentifiers.
var registeredEntityNames = func() map[string]string {
	m := make(map[string]string, len(bedrockEntityIdentifiers))
	for name, id := range bedrockEntityIdentifiers {
		m[id] = name
	}
	return m
}()

// BedrockEntityIdentifier returns the Bedrock identifier of the entity type
// registered as name, which saves and clients use.
func BedrockEntityIdentifier(name string) string {
	if id, ok := bedrockEntityIdentifiers[name]; ok {
		return id
	}
	return name
}

// bedrockToJavaEntity maps Bedrock actor identifiers whose Java entity type
// differs.
var bedrockToJavaEntity = map[string]string{
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
}

// JavaEntityID returns the Java entity type identifier of an entity. Entities
// whose Java type depends on their state, like boats of each wood, report it
// through a JavaEntityID method.
func JavaEntityID(e Entity) string {
	if typed, ok := e.(interface{ JavaEntityID() string }); ok {
		return typed.JavaEntityID()
	}
	return JavaEntityTypeID(e.H().Type().EncodeEntity())
}

// JavaEntityTypeID returns the Java entity type identifier of the entities
// of the Bedrock type identifier passed whose Java type does not depend on
// their state.
func JavaEntityTypeID(id string) string {
	if java, ok := bedrockToJavaEntity[id]; ok {
		return java
	}
	return id
}
