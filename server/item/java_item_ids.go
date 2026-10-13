package item

import (
	"strings"

	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/world"
)

// javaItemAliases maps Java item identifiers to the Bedrock item identifiers
// of the same items where they differ. Bedrock has no spectral arrows, and its
// loot tables give plain arrows in their place.
var javaItemAliases = map[string]string{
	"minecraft:bricks":                       "minecraft:brick_block",
	"minecraft:cobblestone_stairs":           "minecraft:stone_stairs",
	"minecraft:cobweb":                       "minecraft:web",
	"minecraft:dead_bush":                    "minecraft:deadbush",
	"minecraft:end_stone_bricks":             "minecraft:end_bricks",
	"minecraft:end_stone_brick_stairs":       "minecraft:end_brick_stairs",
	"minecraft:flowering_azalea_leaves":      "minecraft:azalea_leaves_flowered",
	"minecraft:jack_o_lantern":               "minecraft:lit_pumpkin",
	"minecraft:light_gray_glazed_terracotta": "minecraft:silver_glazed_terracotta",
	"minecraft:lily_pad":                     "minecraft:waterlily",
	"minecraft:magma_block":                  "minecraft:magma",
	"minecraft:map":                          "minecraft:empty_map",
	"minecraft:melon":                        "minecraft:melon_block",
	"minecraft:nether_bricks":                "minecraft:nether_brick",
	"minecraft:nether_quartz_ore":            "minecraft:quartz_ore",
	"minecraft:note_block":                   "minecraft:noteblock",
	"minecraft:oak_button":                   "minecraft:wooden_button",
	"minecraft:oak_door":                     "minecraft:wooden_door",
	"minecraft:oak_fence_gate":               "minecraft:fence_gate",
	"minecraft:oak_pressure_plate":           "minecraft:wooden_pressure_plate",
	"minecraft:oak_trapdoor":                 "minecraft:trapdoor",
	"minecraft:powered_rail":                 "minecraft:golden_rail",
	"minecraft:prismarine_brick_stairs":      "minecraft:prismarine_bricks_stairs",
	"minecraft:red_nether_bricks":            "minecraft:red_nether_brick",
	"minecraft:rooted_dirt":                  "minecraft:dirt_with_roots",
	"minecraft:shulker_box":                  "minecraft:undyed_shulker_box",
	"minecraft:slime_block":                  "minecraft:slime",
	"minecraft:small_dripleaf":               "minecraft:small_dripleaf_block",
	"minecraft:snow_block":                   "minecraft:snow",
	"minecraft:spectral_arrow":               "minecraft:arrow",
	"minecraft:stone_slab":                   "minecraft:normal_stone_slab",
	"minecraft:terracotta":                   "minecraft:hardened_clay",
	"minecraft:tipped_arrow":                 "minecraft:arrow",
	"minecraft:waxed_copper_block":           "minecraft:waxed_copper",
}

// javaItem resolves a namespaced Java item identifier to the registered
// Bedrock item. Coloured beds and banners are one Bedrock item each, told
// apart by metadata.
func javaItem(id string) (world.Item, bool) {
	if alias, ok := javaItemAliases[id]; ok {
		id = alias
	}
	if it, ok := world.ItemByName(id, 0); ok {
		return it, true
	}
	for _, c := range Colours() {
		switch strings.TrimPrefix(id, "minecraft:"+c.String()+"_") {
		case "bed":
			return world.ItemByName("minecraft:bed", int16(c.Uint8()))
		case "banner":
			return world.ItemByName("minecraft:banner", ^int16(c.Uint8())&0xf)
		}
	}
	return nil, false
}

// javaPotionContents ports the potion_contents component onto potions, splash
// and lingering potions and tipped arrows: the component is the potion
// identifier or a compound holding it.
func javaPotionContents(s Stack, value any) Stack {
	id, ok := value.(string)
	if contents, isMap := value.(map[string]any); isMap {
		id, ok = contents["potion"].(string)
	}
	if !ok {
		return s
	}
	p, ok := potion.FromJavaID(id)
	if !ok {
		return s
	}
	switch s.Item().(type) {
	case Potion:
		return s.WithItem(Potion{Type: p})
	case SplashPotion:
		return s.WithItem(SplashPotion{Type: p})
	case LingeringPotion:
		return s.WithItem(LingeringPotion{Type: p})
	case Arrow:
		return s.WithItem(Arrow{Tip: p})
	}
	return s
}
