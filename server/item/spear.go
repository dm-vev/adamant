package item

import (

	"github.com/df-mc/dragonfly/server/item/category"
)

// Spear is a kinetic weapon. It is held to build momentum and attacks every entity in its stab path.
// The item carries the Java 26 kinetic weapon parameters used by player combat dispatch.
type Spear struct {
	// Tier is the material the spear is made from.
	Tier ToolTier
}


// KineticWeaponInfo returns the Java 26 kinetic parameters for the spear material.
func (s Spear) KineticWeaponInfo() KineticWeaponInfo {
	info := KineticWeaponInfo{ContactCooldownTicks: 10, ForwardMovement: 0.38}
	switch s.Tier {
	case ToolTierWood:
		info.DelayTicks, info.DamageMultiplier, info.AttackDuration = 15, 0.7, 0.65
		info.Dismount = KineticWeaponCondition{MaxDurationTicks: 100, MinSpeed: 14}
		info.Knockback = KineticWeaponCondition{MaxDurationTicks: 200, MinSpeed: 5.1}
		info.Damage = KineticWeaponCondition{MaxDurationTicks: 300, MinRelativeSpeed: 4.6}
	case ToolTierStone:
		info.DelayTicks, info.DamageMultiplier, info.AttackDuration = 14, 0.82, 0.75
		info.Dismount = KineticWeaponCondition{MaxDurationTicks: 90, MinSpeed: 13}
		info.Knockback = KineticWeaponCondition{MaxDurationTicks: 180, MinSpeed: 5.1}
		info.Damage = KineticWeaponCondition{MaxDurationTicks: 275, MinRelativeSpeed: 4.6}
	case ToolTierCopper:
		info.DelayTicks, info.DamageMultiplier, info.AttackDuration = 13, 0.82, 0.85
		info.Dismount = KineticWeaponCondition{MaxDurationTicks: 80, MinSpeed: 12}
		info.Knockback = KineticWeaponCondition{MaxDurationTicks: 165, MinSpeed: 5.1}
		info.Damage = KineticWeaponCondition{MaxDurationTicks: 250, MinRelativeSpeed: 4.6}
	case ToolTierIron:
		info.DelayTicks, info.DamageMultiplier, info.AttackDuration = 12, 0.95, 0.95
		info.Dismount = KineticWeaponCondition{MaxDurationTicks: 50, MinSpeed: 11}
		info.Knockback = KineticWeaponCondition{MaxDurationTicks: 135, MinSpeed: 5.1}
		info.Damage = KineticWeaponCondition{MaxDurationTicks: 225, MinRelativeSpeed: 4.6}
	case ToolTierGold:
		info.DelayTicks, info.DamageMultiplier, info.AttackDuration = 14, 0.7, 0.95
		info.Dismount = KineticWeaponCondition{MaxDurationTicks: 70, MinSpeed: 13}
		info.Knockback = KineticWeaponCondition{MaxDurationTicks: 170, MinSpeed: 5.1}
		info.Damage = KineticWeaponCondition{MaxDurationTicks: 275, MinRelativeSpeed: 4.6}
	case ToolTierDiamond:
		info.DelayTicks, info.DamageMultiplier, info.AttackDuration = 10, 1.075, 1.05
		info.Dismount = KineticWeaponCondition{MaxDurationTicks: 60, MinSpeed: 10}
		info.Knockback = KineticWeaponCondition{MaxDurationTicks: 130, MinSpeed: 5.1}
		info.Damage = KineticWeaponCondition{MaxDurationTicks: 200, MinRelativeSpeed: 4.6}
	case ToolTierNetherite:
		info.DelayTicks, info.DamageMultiplier, info.AttackDuration = 8, 1.2, 1.15
		info.Dismount = KineticWeaponCondition{MaxDurationTicks: 50, MinSpeed: 9}
		info.Knockback = KineticWeaponCondition{MaxDurationTicks: 110, MinSpeed: 5.1}
		info.Damage = KineticWeaponCondition{MaxDurationTicks: 175, MinRelativeSpeed: 4.6}
	}
	return info
}

// Matches reports whether the condition is active at the given use tick and speeds.
func (c KineticWeaponCondition) Matches(ticksUsed int, attackerSpeed, relativeSpeed, actionFactor float64) bool {
	return ticksUsed <= c.MaxDurationTicks && attackerSpeed >= c.MinSpeed*actionFactor && relativeSpeed >= c.MinRelativeSpeed*actionFactor
}

// PiercingWeaponInfo configures the Java spear's normal piercing attack.
func (Spear) PiercingWeaponInfo() PiercingWeaponInfo {
	return PiercingWeaponInfo{DealsKnockback: true}
}

// MaxCount always returns 1.
func (Spear) MaxCount() int { return 1 }

// AttackDamage returns the attack damage associated with the spear's material.
func (s Spear) AttackDamage() float64 { return float64(s.Tier.BaseAttackDamage) }

// EnchantmentValue returns the enchantability of the material.
func (s Spear) EnchantmentValue() int { return s.Tier.EnchantmentValue }

// HandEquipped makes the spear render as a held weapon.
func (Spear) HandEquipped() bool { return true }

// DurabilityInfo returns the material durability; Java weapon attacks consume one durability point.
func (s Spear) DurabilityInfo() DurabilityInfo {
	return DurabilityInfo{MaxDurability: s.Tier.Durability, BrokenItem: simpleItem(Stack{}), AttackDurability: 1}
}

// RepairableBy reports whether the item can be repaired with its material's repair ingredient.
func (s Spear) RepairableBy(i Stack) bool { return toolTierRepairable(s.Tier)(i) }

// FireProof reports whether the spear is made from netherite.
func (s Spear) FireProof() bool { return s.Tier == ToolTierNetherite }

// EncodeItem returns the Bedrock registry item name.
func (s Spear) EncodeItem() (string, int16) { return "minecraft:" + s.Tier.Name + "_spear", 0 }

// Category returns the equipment category used by weapons.
func (Spear) Category() category.Category { return category.Equipment().WithGroup("itemGroup.name.sword") }


