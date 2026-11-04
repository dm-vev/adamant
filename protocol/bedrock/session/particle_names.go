package session

// bedrockParticleNames maps the particle types of Java Edition the game
// shows by name, client.ViewNamedParticle, to the Bedrock particle effects
// clients show for them. Names missing from the table, such as Bedrock
// identifiers themselves and those of the particles of resource packs, are
// sent as they are.
var bedrockParticleNames = map[string]string{
	"minecraft:angry_villager":    "minecraft:villager_angry",
	"minecraft:cloud":             "minecraft:basic_smoke_particle",
	"minecraft:end_rod":           "minecraft:endrod",
	"minecraft:explosion":         "minecraft:huge_explosion_lab_misc_emitter",
	"minecraft:explosion_emitter": "minecraft:huge_explosion_lab_misc_emitter",
	"minecraft:flame":             "minecraft:basic_flame_particle",
	"minecraft:happy_villager":    "minecraft:villager_happy",
	"minecraft:heart":             "minecraft:heart_particle",
	"minecraft:large_smoke":       "minecraft:basic_smoke_particle",
	"minecraft:poof":              "minecraft:basic_smoke_particle",
	"minecraft:portal":            "minecraft:portal_directional",
	"minecraft:smoke":             "minecraft:basic_smoke_particle",
	"minecraft:soul_fire_flame":   "minecraft:soul_particle",
	"minecraft:totem_of_undying":  "minecraft:totem_particle",
}

// bedrockParticleName returns the Bedrock particle effect of the name a
// particle is shown by in the game.
func bedrockParticleName(name string) string {
	if b, ok := bedrockParticleNames[name]; ok {
		return b
	}
	return name
}
