package player

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
)

func java26MobDamageBonus(stack item.Stack, target string) float64 {
	var bonus float64
	if level, ok := stack.Enchantment(enchantment.Smite); ok && undeadEntity(target) {
		bonus += enchantment.Smite.Addend(level.Level())
	}
	if level, ok := stack.Enchantment(enchantment.BaneOfArthropods); ok && arthropodEntity(target) {
		bonus += enchantment.BaneOfArthropods.Addend(level.Level())
	}
	return bonus
}

func undeadEntity(id string) bool {
	switch id {
	case "minecraft:zombie", "minecraft:husk", "minecraft:drowned", "minecraft:zombie_villager", "minecraft:zombie_horse", "minecraft:skeleton", "minecraft:stray", "minecraft:wither_skeleton", "minecraft:skeleton_horse", "minecraft:wither", "minecraft:phantom", "minecraft:zombified_piglin", "minecraft:piglin_brute", "minecraft:zoglin", "minecraft:bogged", "minecraft:giant", "minecraft:zombie_nautilus":
		return true
	default:
		return false
	}
}

func arthropodEntity(id string) bool {
	switch id {
	case "minecraft:spider", "minecraft:cave_spider", "minecraft:bee", "minecraft:silverfish", "minecraft:endermite":
		return true
	default:
		return false
	}
}
