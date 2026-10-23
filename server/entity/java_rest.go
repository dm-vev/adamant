package entity

import "github.com/df-mc/dragonfly/server/world"

// IsPreventingPlayerRest follows Java Monster's predicate, including its sole
// ZombifiedPiglin override. Enemy/spawn category is not equivalent to Monster:
// phantoms, slimes, magma cubes and hoglins do not participate in this query.
func (e *Ent) IsPreventingPlayerRest(player world.Entity) bool {
	switch e.H().Type().EncodeEntity() {
	case "minecraft:zombie_pigman", "minecraft:zombified_piglin":
		// NeutralMob.isAngryAt, including universal_anger.
		s := e.data.Data.(*zombifiedPiglinState)
		return s.angryAt(s.ai.Mob(e, &ZombifiedPiglin{e}), player)
	case "minecraft:blaze", "minecraft:creeper", "minecraft:elder_guardian", "minecraft:enderman", "minecraft:endermite", "minecraft:giant", "minecraft:guardian", "minecraft:silverfish", "minecraft:vex", "minecraft:witch", "minecraft:ravager", "minecraft:zoglin", "minecraft:breeze", "minecraft:creaking", "minecraft:evocation_illager", "minecraft:evoker", "minecraft:vindicator", "minecraft:vindication_illager", "minecraft:pillager", "minecraft:illusioner", "minecraft:piglin", "minecraft:piglin_brute", "minecraft:bogged", "minecraft:parched", "minecraft:skeleton", "minecraft:stray", "minecraft:wither_skeleton", "minecraft:spider", "minecraft:cave_spider", "minecraft:warden", "minecraft:zombie", "minecraft:zombie_villager", "minecraft:zombie_villager_v2", "minecraft:husk", "minecraft:drowned", "minecraft:wither":
		return true
	}
	return false
}
