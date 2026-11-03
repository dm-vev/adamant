package session

import (
	"cmp"
	"slices"

	"github.com/df-mc/dragonfly/server/world"
)

// bdsActorIdentifiers is the order in which BDS 1.26.52.3 lists the types of
// actors in AvailableActorIdentifiers, the same on every join and neither by
// runtime ID nor by name, as recorded in
// testdata/bedrock_1_26_52_actor_order.json.
var bdsActorIdentifiers = []string{
	"minecraft:player", "minecraft:villager_v2", "minecraft:villager",
	"minecraft:arrow", "minecraft:thrown_trident",
	"minecraft:wind_charge_projectile",
	"minecraft:breeze_wind_charge_projectile", "minecraft:ice_bomb",
	"minecraft:evocation_fang", "minecraft:llama_spit",
	"minecraft:lingering_potion", "minecraft:small_fireball",
	"minecraft:wither_skull_dangerous", "minecraft:wither_skull",
	"minecraft:ender_pearl", "minecraft:splash_potion", "minecraft:fireball",
	"minecraft:egg", "minecraft:snowball", "minecraft:dragon_fireball",
	"minecraft:shulker_bullet", "minecraft:xp_bottle", "minecraft:zombie_horse",
	"minecraft:skeleton_horse", "minecraft:mule", "minecraft:donkey",
	"minecraft:horse", "minecraft:parched", "minecraft:bogged",
	"minecraft:wither_skeleton", "minecraft:stray", "minecraft:skeleton",
	"minecraft:command_block_minecart", "minecraft:chest_minecart",
	"minecraft:tnt_minecart", "minecraft:hopper_minecart", "minecraft:minecart",
	"minecraft:endermite", "minecraft:cave_spider", "minecraft:silverfish",
	"minecraft:husk", "minecraft:zombie_villager", "minecraft:camel_husk",
	"minecraft:phantom", "minecraft:zombie_villager_v2", "minecraft:bat",
	"minecraft:cat", "minecraft:ocelot", "minecraft:wither",
	"minecraft:nautilus", "minecraft:zombie_pigman", "minecraft:tadpole",
	"minecraft:zombie", "minecraft:glow_squid", "minecraft:cod",
	"minecraft:wolf", "minecraft:tropicalfish", "minecraft:trader_llama",
	"minecraft:spider", "minecraft:frog", "minecraft:dolphin", "minecraft:goat",
	"minecraft:hoglin", "minecraft:turtle", "minecraft:zoglin",
	"minecraft:llama", "minecraft:strider", "minecraft:polar_bear",
	"minecraft:rabbit", "minecraft:panda", "minecraft:mooshroom",
	"minecraft:drowned", "minecraft:sheep", "minecraft:salmon", "minecraft:pig",
	"minecraft:pufferfish", "minecraft:cow", "minecraft:warden",
	"minecraft:elder_guardian_ghost", "minecraft:vex",
	"minecraft:evocation_illager", "minecraft:cushion", "minecraft:vindicator",
	"minecraft:zombie_nautilus", "minecraft:ender_dragon",
	"minecraft:happy_ghast", "minecraft:elder_guardian", "minecraft:creaking",
	"minecraft:guardian", "minecraft:armadillo", "minecraft:witch",
	"minecraft:sniffer", "minecraft:magma_cube", "minecraft:enderman",
	"minecraft:allay", "minecraft:slime", "minecraft:ravager",
	"minecraft:agent", "minecraft:sulfur_cube", "minecraft:shulker",
	"minecraft:npc", "minecraft:copper_golem", "minecraft:wandering_trader",
	"minecraft:chest_boat", "minecraft:snow_golem", "minecraft:squid",
	"minecraft:pillager", "minecraft:iron_golem", "minecraft:axolotl",
	"minecraft:creeper", "minecraft:parrot", "minecraft:piglin_brute",
	"minecraft:ominous_item_spawner", "minecraft:fishing_hook", "minecraft:fox",
	"minecraft:tripod_camera", "minecraft:chicken", "minecraft:balloon",
	"minecraft:bee", "minecraft:armor_stand", "minecraft:breeze",
	"minecraft:blaze", "minecraft:boat", "minecraft:painting",
	"minecraft:area_effect_cloud", "minecraft:fireworks_rocket",
	"minecraft:ender_crystal", "minecraft:tnt", "minecraft:camel",
	"minecraft:ghast", "minecraft:leash_knot", "minecraft:piglin",
	"minecraft:falling_block", "minecraft:lightning_bolt",
	"minecraft:eye_of_ender_signal", "minecraft:xp_orb", "minecraft:item",
}

// bdsActorProperties is the order in which BDS 1.26.52.3 sends the
// SyncActorProperty packets, by the type each declares, the same on every
// join and another than that of bdsActorIdentifiers, as recorded in
// testdata/bedrock_1_26_52_actor_order.json.
var bdsActorProperties = []string{
	"minecraft:sulfur_cube", "minecraft:wolf", "minecraft:armadillo",
	"minecraft:pig", "minecraft:happy_ghast", "minecraft:cat",
	"minecraft:creaking", "minecraft:zombie_nautilus", "minecraft:chicken",
	"minecraft:bee", "minecraft:egg", "minecraft:cow", "minecraft:copper_golem",
}

// rankOf maps each identifier of order to its index.
func rankOf(order []string) map[string]int {
	m := make(map[string]int, len(order))
	for i, id := range order {
		m[id] = i
	}
	return m
}

var (
	bdsActorIdentifierRank = rankOf(bdsActorIdentifiers)
	bdsActorPropertyRank   = rankOf(bdsActorProperties)
)

// actorTypesInOrder returns the network IDs of the types of the registry, one
// per type, in the order of rank (see orderActorIDs).
func actorTypesInOrder(reg world.EntityRegistry, rank map[string]int) []string {
	var ids []string
	for _, t := range reg.Types() {
		ids = append(ids, networkID(t))
	}
	orderActorIDs(ids, rank)
	return ids
}

// orderActorIDs sorts network IDs of actors as BDS orders those it has, by
// rank, and puts those BDS does not have after them, by ID. Equal IDs, of
// types that share one, end up adjacent.
func orderActorIDs(ids []string, rank map[string]int) {
	slices.SortFunc(ids, func(a, b string) int {
		ra, oka := rank[a]
		rb, okb := rank[b]
		switch {
		case oka && okb:
			return cmp.Compare(ra, rb)
		case oka:
			return -1
		case okb:
			return 1
		}
		return cmp.Compare(a, b)
	})
}
