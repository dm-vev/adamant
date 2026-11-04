package entity

type naturalSpawnCategory uint8

const (
	spawnCreature naturalSpawnCategory = iota
	spawnMonster
	spawnAmbient
	spawnWaterCreature
	spawnWaterAmbient
	spawnUndergroundWaterCreature
	spawnAxolotls
	spawnMisc
)

// mobCategories are the MobCategory of the entity types that are not MISC,
// as EntityTypes registers them.
var mobCategories = func() map[string]naturalSpawnCategory {
	m := map[string]naturalSpawnCategory{}
	for category, ids := range map[naturalSpawnCategory][]string{
		spawnCreature: {"allay", "armadillo", "bee", "camel", "cat", "chicken", "cow", "donkey", "fox", "frog", "goat", "happy_ghast", "horse",
			"llama", "mooshroom", "mule", "ocelot", "panda", "parrot", "pig", "polar_bear", "rabbit", "sheep", "skeleton_horse", "sniffer",
			"strider", "tadpole", "trader_llama", "turtle", "wandering_trader", "wolf"},
		spawnAxolotls: {"axolotl"},
		spawnAmbient:  {"bat"},
		spawnMonster: {"blaze", "bogged", "breeze", "camel_husk", "cave_spider", "creaking", "creeper", "drowned", "elder_guardian",
			"ender_dragon", "enderman", "endermite", "evoker", "ghast", "giant", "guardian", "hoglin", "husk", "illusioner", "magma_cube",
			"parched", "phantom", "piglin", "piglin_brute", "pillager", "ravager", "shulker", "silverfish", "skeleton", "slime", "spider",
			"stray", "sulfur_cube", "vex", "vindicator", "warden", "witch", "wither", "wither_skeleton", "zoglin", "zombie", "zombie_horse",
			"zombie_nautilus", "zombie_villager", "zombified_piglin"},
		spawnWaterAmbient:             {"cod", "pufferfish", "salmon", "tropical_fish"},
		spawnWaterCreature:            {"dolphin", "nautilus", "squid"},
		spawnUndergroundWaterCreature: {"glow_squid"},
	} {
		for _, id := range ids {
			m["minecraft:"+id] = category
		}
	}
	return m
}()
