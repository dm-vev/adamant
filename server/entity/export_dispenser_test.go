package entity

import "github.com/df-mc/dragonfly/server/world"

// PiglinArmourDropChances returns the drop chance of the head, chest, legs and
// feet of a piglin.
func PiglinArmourDropChances(e world.Entity) [4]float32 {
	h, _ := equipmentHolderOf(e)
	return [4]float32{h.dropChance(slotHead), h.dropChance(slotChest), h.dropChance(slotLegs), h.dropChance(slotFeet)}
}
