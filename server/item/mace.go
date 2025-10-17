package item

import (
	"github.com/df-mc/dragonfly/server/item/category"
	"math"
)

// Mace is a heavy melee weapon that deals increased damage when smashing into targets after a fall.
type Mace struct{}

// MaxCount always returns 1.
func (Mace) MaxCount() int {
	return 1
}

// AttackDamage returns the base attack damage of the mace.
func (Mace) AttackDamage() float64 {
	return 6
}

// EnchantmentValue returns the enchantment value of the mace.
func (Mace) EnchantmentValue() int {
	return 15
}

// DurabilityInfo returns the durability configuration of the mace.
func (Mace) DurabilityInfo() DurabilityInfo {
	return DurabilityInfo{
		MaxDurability:    500,
		BrokenItem:       simpleItem(Stack{}),
		AttackDurability: 1,
		BreakDurability:  2,
	}
}

// HandEquipped makes sure the mace renders like a weapon when held.
func (Mace) HandEquipped() bool {
	return true
}

// RepairableBy reports if the mace may be repaired using the provided stack.
func (Mace) RepairableBy(i Stack) bool {
	return matchesItemName(i, "minecraft:breeze_rod") || matchesItemName(i, "minecraft:mace")
}

// Category returns the creative inventory category the mace resides in.
func (Mace) Category() category.Category {
	return category.Equipment().WithGroup("itemGroup.name.sword")
}

// EncodeItem ...
func (Mace) EncodeItem() (name string, meta int16) {
	return "minecraft:mace", 0
}

// matchesItemName checks if the item stack has the supplied identifier.
func matchesItemName(stack Stack, name string) bool {
	if stack.Empty() {
		return false
	}
	n, _ := stack.Item().EncodeItem()
	return n == name
}

// SmashBonus calculates the bonus damage granted by a mace smash for a fall distance measured in blocks.
func (Mace) SmashBonus(distance float64) float64 {
	if distance <= 0 {
		return 0
	}
	remaining := distance
	bonus := 0.0

	firstTier := math.Min(remaining, 3)
	bonus += firstTier * 4
	remaining -= firstTier

	if remaining <= 0 {
		return bonus
	}

	secondTier := math.Min(remaining, 5)
	bonus += secondTier * 2
	remaining -= secondTier

	if remaining <= 0 {
		return bonus
	}

	bonus += remaining
	return bonus
}

// EffectiveFallDistance clamps the fall distance that may contribute towards smash damage.
func (Mace) EffectiveFallDistance(distance float64) float64 {
	if distance < 0 {
		return 0
	}
	return distance
}

// SmashThreshold returns the minimum fall distance required to activate smash effects.
func (Mace) SmashThreshold() float64 {
	return 1.5
}
