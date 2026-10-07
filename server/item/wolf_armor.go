package item

import (
	"image/color"

	"github.com/df-mc/dragonfly/server/world"
)

// WolfArmor is body armour that can be equipped on a tamed wolf.
type WolfArmor struct {
	Colour color.RGBA
}

// MaxCount returns the wolf armour stack limit.
func (WolfArmor) MaxCount() int { return 1 }

// EncodeItem returns the Bedrock item identifier.
func (WolfArmor) EncodeItem() (string, int16) { return "minecraft:wolf_armor", 0 }

// DefencePoints is the armour value provided to a wolf wearing this item.
func (WolfArmor) DefencePoints() float64 { return 11 }

// Toughness returns this armour's toughness.
func (WolfArmor) Toughness() float64 { return 0 }

// KnockBackResistance returns this armour's knockback resistance.
func (WolfArmor) KnockBackResistance() float64 { return 0 }

// DurabilityInfo describes the durability of wolf armour.
func (WolfArmor) DurabilityInfo() DurabilityInfo {
	return DurabilityInfo{MaxDurability: 64, BrokenItem: simpleItem(Stack{}), BreakDurability: 1}
}

// RepairableBy reports whether armadillo scutes repair wolf armour.
func (WolfArmor) RepairableBy(stack Stack) bool {
	_, ok := stack.Item().(ArmadilloScute)
	return ok
}

// UseOnEntity equips the armour if the target accepts it.
func (w WolfArmor) UseOnEntity(target world.Entity, tx *world.Tx, user User, ctx *UseContext) bool {
	mob, ok := target.(interface {
		CanEquipBodyArmor(world.Entity) bool
		EquipBodyArmor(*world.Tx, world.Entity, world.Item) bool
	})
	if !ok || !mob.CanEquipBodyArmor(user) || !mob.EquipBodyArmor(tx, user, w) {
		return false
	}
	ctx.SubtractFromCount(1)
	return true
}
// EncodeNBT preserves the wolf armour's custom dye colour.
func (w WolfArmor) EncodeNBT() map[string]any {
	if w.Colour == (color.RGBA{}) {
		return nil
	}
	return map[string]any{"customColor": int32FromRGBA(w.Colour)}
}

// DecodeNBT restores the wolf armour's custom dye colour.
func (w WolfArmor) DecodeNBT(data map[string]any) any {
	if colour, ok := data["customColor"].(int32); ok {
		w.Colour = rgbaFromInt32(colour)
	}
	return w
}
