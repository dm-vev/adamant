package item

import "github.com/df-mc/dragonfly/server/world"

// NautilusArmorMaterial identifies the material used by Nautilus armour.
type NautilusArmorMaterial uint8

const (
	NautilusArmorCopper NautilusArmorMaterial = iota
	NautilusArmorIron
	NautilusArmorGold
	NautilusArmorDiamond
	NautilusArmorNetherite
)

// NautilusArmor is body armour equipable by Nautiluses and Zombie Nautiluses.
type NautilusArmor struct {
	Material NautilusArmorMaterial
}

// MaxCount returns the Java stack limit for Nautilus armour.
func (NautilusArmor) MaxCount() int { return 1 }

// EncodeItem returns the material-specific item identifier.
func (a NautilusArmor) EncodeItem() (string, int16) {
	name := [...]string{"copper", "iron", "golden", "diamond", "netherite"}[min(a.Material, NautilusArmorNetherite)]
	return "minecraft:" + name + "_nautilus_armor", 0
}

// DefencePoints returns the body-armour protection for this material.
func (a NautilusArmor) DefencePoints() float64 {
	return [...]float64{4, 5, 7, 11, 19}[min(a.Material, NautilusArmorNetherite)]
}

// Toughness returns this material's armour toughness.
func (a NautilusArmor) Toughness() float64 {
	return [...]float64{0, 0, 0, 2, 3}[min(a.Material, NautilusArmorNetherite)]
}

// KnockBackResistance returns this material's knockback resistance.
func (a NautilusArmor) KnockBackResistance() float64 {
	if a.Material == NautilusArmorNetherite {
		return 0.1
	}
	return 0
}

// FireProof reports whether the armour is made from netherite.
func (a NautilusArmor) FireProof() bool { return a.Material == NautilusArmorNetherite }

// UseOnEntity equips the armour on a Nautilus.
func (a NautilusArmor) UseOnEntity(target world.Entity, tx *world.Tx, user User, ctx *UseContext) bool {
	mob, ok := target.(interface {
		CanEquipBodyArmor(world.Entity) bool
		EquipBodyArmor(*world.Tx, world.Entity, world.Item) bool
	})
	if !ok || !mob.CanEquipBodyArmor(user) || !mob.EquipBodyArmor(tx, user, a) {
		return false
	}
	ctx.SubtractFromCount(1)
	return true
}
