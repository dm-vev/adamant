package session

import (
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
)

// networkIDs is the identifier clients know each type of the default entity
// registry by, and how far it is shown, frozen from the commit before the
// entity package stopped naming types for the network.
var networkIDs = []struct {
	name, network string
	tracking      float64
}{
	{"minecraft:zombie_horse", "minecraft:zombie_horse", 128},
	{"minecraft:arrow", "minecraft:arrow", 64},
	{"minecraft:fishing_hook", "minecraft:fishing_hook", 64},
	{"minecraft:boat", "minecraft:boat", 160},
	{"minecraft:chicken", "minecraft:chicken", 128},
	{"minecraft:slime", "minecraft:slime", 128},
	{"minecraft:evoker_fangs", "minecraft:evocation_fang", 128},
	{"minecraft:panda", "minecraft:panda", 128},
	{"minecraft:bogged", "minecraft:bogged", 128},
	{"minecraft:nautilus", "minecraft:nautilus", 128},
	{"minecraft:iron_golem", "minecraft:iron_golem", 160},
	{"minecraft:snowball", "minecraft:snowball", 64},
	{"minecraft:sulfur_cube", "minecraft:sulfur_cube", 128},
	{"minecraft:area_effect_cloud", "minecraft:area_effect_cloud", 160},
	{"minecraft:chest_minecart", "minecraft:chest_minecart", 128},
	{"minecraft:villager", "minecraft:villager_v2", 128},
	{"minecraft:cod", "minecraft:cod", 128},
	{"minecraft:mooshroom", "minecraft:mooshroom", 128},
	{"minecraft:salmon", "minecraft:salmon", 128},
	{"minecraft:command_block_minecart", "minecraft:command_block_minecart", 128},
	{"minecraft:frog", "minecraft:frog", 128},
	{"minecraft:rabbit", "minecraft:rabbit", 128},
	{"minecraft:stray", "minecraft:stray", 128},
	{"minecraft:zoglin", "minecraft:zoglin", 128},
	{"minecraft:bat", "minecraft:bat", 128},
	{"minecraft:cushion", "minecraft:cushion", 128},
	{"minecraft:ender_crystal", "minecraft:ender_crystal", 256},
	{"minecraft:thrown_trident", "minecraft:thrown_trident", 64},
	{"minecraft:eye_of_ender_signal", "minecraft:eye_of_ender_signal", 64},
	{"minecraft:squid", "minecraft:squid", 128},
	{"minecraft:llama_spit", "minecraft:llama_spit", 64},
	{"minecraft:donkey", "minecraft:donkey", 128},
	{"minecraft:pig", "minecraft:pig", 128},
	{"minecraft:zombie", "minecraft:zombie", 128},
	{"minecraft:strider", "minecraft:strider", 128},
	{"minecraft:armor_stand", "minecraft:armor_stand", 160},
	{"minecraft:tropical_fish", "minecraft:tropicalfish", 128},
	{"minecraft:ender_dragon", "minecraft:ender_dragon", 160},
	{"minecraft:painting", "minecraft:painting", 160},
	{"minecraft:vex", "minecraft:vex", 128},
	{"minecraft:cow", "minecraft:cow", 128},
	{"minecraft:tnt", "minecraft:tnt", 160},
	{"minecraft:ravager", "minecraft:ravager", 128},
	{"minecraft:cat", "minecraft:cat", 128},
	{"minecraft:chest_boat", "minecraft:chest_boat", 160},
	{"minecraft:lightning_bolt", "minecraft:lightning_bolt", 256},
	{"minecraft:zombie_pigman", "minecraft:zombie_pigman", 128},
	{"minecraft:allay", "minecraft:allay", 128},
	{"minecraft:zombie_nautilus", "minecraft:zombie_nautilus", 128},
	{"minecraft:wither_skeleton", "minecraft:wither_skeleton", 128},
	{"minecraft:magma_cube", "minecraft:magma_cube", 128},
	{"minecraft:snow_golem", "minecraft:snow_golem", 128},
	{"minecraft:zombie_villager", "minecraft:zombie_villager_v2", 128},
	{"minecraft:cave_spider", "minecraft:cave_spider", 128},
	{"minecraft:parched", "minecraft:parched", 128},
	{"minecraft:piglin_brute", "minecraft:piglin_brute", 128},
	{"minecraft:copper_golem", "minecraft:copper_golem", 128},
	{"minecraft:axolotl", "minecraft:axolotl", 128},
	{"minecraft:shulker", "minecraft:shulker", 128},
	{"minecraft:splash_potion", "minecraft:splash_potion", 64},
	{"minecraft:skeleton_horse", "minecraft:skeleton_horse", 128},
	{"minecraft:llama", "minecraft:llama", 128},
	{"minecraft:breeze_wind_charge", "minecraft:breeze_wind_charge_projectile", 64},
	{"minecraft:drowned", "minecraft:drowned", 128},
	{"minecraft:falling_block", "minecraft:falling_block", 160},
	{"minecraft:dragon_fireball", "minecraft:dragon_fireball", 64},
	{"minecraft:trader_llama", "minecraft:trader_llama", 128},
	{"minecraft:sheep", "minecraft:sheep", 128},
	{"minecraft:sniffer", "minecraft:sniffer", 128},
	{"minecraft:silverfish", "minecraft:silverfish", 128},
	{"minecraft:glow_squid", "minecraft:glow_squid", 128},
	{"minecraft:parrot", "minecraft:parrot", 128},
	{"dragonfly:text", "minecraft:falling_block", 160},
	{"minecraft:enderman", "minecraft:enderman", 128},
	{"minecraft:elder_guardian", "minecraft:elder_guardian", 128},
	{"minecraft:hopper_minecart", "minecraft:hopper_minecart", 128},
	{"minecraft:wind_charge_projectile", "minecraft:wind_charge_projectile", 64},
	{"minecraft:ghast", "minecraft:ghast", 160},
	{"minecraft:xp_bottle", "minecraft:xp_bottle", 64},
	{"minecraft:spider", "minecraft:spider", 128},
	{"minecraft:vindicator", "minecraft:vindicator", 128},
	{"minecraft:ominous_item_spawner", "minecraft:ominous_item_spawner", 128},
	{"minecraft:fox", "minecraft:fox", 128},
	{"minecraft:mule", "minecraft:mule", 128},
	{"minecraft:skeleton", "minecraft:skeleton", 128},
	{"minecraft:pillager", "minecraft:pillager", 128},
	{"minecraft:ocelot", "minecraft:ocelot", 128},
	{"minecraft:egg", "minecraft:egg", 64},
	{"minecraft:shulker_bullet", "minecraft:shulker_bullet", 128},
	{"minecraft:camel", "minecraft:camel", 128},
	{"minecraft:endermite", "minecraft:endermite", 128},
	{"minecraft:turtle", "minecraft:turtle", 128},
	{"minecraft:happy_ghast", "minecraft:happy_ghast", 160},
	{"minecraft:witch", "minecraft:witch", 128},
	{"minecraft:creeper", "minecraft:creeper", 128},
	{"minecraft:husk", "minecraft:husk", 128},
	{"minecraft:wither", "minecraft:wither", 160},
	{"minecraft:blaze", "minecraft:blaze", 128},
	{"minecraft:polar_bear", "minecraft:polar_bear", 128},
	{"minecraft:breeze", "minecraft:breeze", 128},
	{"minecraft:guardian", "minecraft:guardian", 128},
	{"minecraft:wolf", "minecraft:wolf", 128},
	{"minecraft:item", "minecraft:item", 96},
	{"minecraft:goat", "minecraft:goat", 128},
	{"minecraft:ender_pearl", "minecraft:ender_pearl", 64},
	{"minecraft:horse", "minecraft:horse", 128},
	{"minecraft:wither_skull", "minecraft:wither_skull", 64},
	{"minecraft:piglin", "minecraft:piglin", 128},
	{"minecraft:dolphin", "minecraft:dolphin", 128},
	{"minecraft:fireworks_rocket", "minecraft:fireworks_rocket", 64},
	{"minecraft:tadpole", "minecraft:tadpole", 128},
	{"minecraft:lingering_potion", "minecraft:lingering_potion", 64},
	{"minecraft:leash_knot", "minecraft:leash_knot", 160},
	{"minecraft:camel_husk", "minecraft:camel_husk", 128},
	{"minecraft:pufferfish", "minecraft:pufferfish", 128},
	{"minecraft:xp_orb", "minecraft:xp_orb", 96},
	{"minecraft:minecart", "minecraft:minecart", 128},
	{"minecraft:tnt_minecart", "minecraft:tnt_minecart", 128},
	{"minecraft:fireball", "minecraft:fireball", 64},
	{"minecraft:armadillo", "minecraft:armadillo", 128},
	{"minecraft:warden", "minecraft:warden", 256},
	{"minecraft:hoglin", "minecraft:hoglin", 128},
	{"minecraft:bee", "minecraft:bee", 128},
	{"minecraft:wandering_trader", "minecraft:wandering_trader", 128},
	{"minecraft:evoker", "minecraft:evocation_illager", 128},
	{"minecraft:creaking", "minecraft:creaking", 128},
	{"minecraft:small_fireball", "minecraft:small_fireball", 64},
	{"minecraft:phantom", "minecraft:phantom", 128},
}

// TestNetworkIDsUnchanged holds networkID and typeRange of every type of
// the default entity registry to the frozen table.
func TestNetworkIDsUnchanged(t *testing.T) {
	types := map[string]world.EntityType{}
	for _, ty := range entity.DefaultRegistry.Types() {
		types[ty.EncodeEntity()] = ty
	}
	if len(types) != len(networkIDs) {
		t.Fatalf("the default registry has %d types, the table %d", len(types), len(networkIDs))
	}
	for _, want := range networkIDs {
		ty, ok := types[want.name]
		if !ok {
			t.Errorf("the default registry has no type %q", want.name)
			continue
		}
		if got := networkID(ty); got != want.network {
			t.Errorf("networkID(%q) = %q, want %q", want.name, got, want.network)
		}
		if got := typeRange(ty); got != want.tracking {
			t.Errorf("typeRange(%q) = %v, want %v", want.name, got, want.tracking)
		}
	}
}
