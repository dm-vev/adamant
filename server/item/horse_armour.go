package item

import "image/color"

// horseArmour is a shared implementation for the various horse armour items.
type horseArmour struct{}

// MaxCount always returns 1.
func (horseArmour) MaxCount() int {
	return 1
}

// LeatherHorseArmour is a type of horse armour that offers minimal protection.
type LeatherHorseArmour struct {
	horseArmour
	// Colour is the dyed colour of the armour. The zero colour is undyed.
	Colour color.RGBA
}

// EncodeItem ...
func (LeatherHorseArmour) EncodeItem() (string, int16) {
	return "minecraft:leather_horse_armor", 0
}

// EncodeNBT preserves the armour's custom dye colour.
func (l LeatherHorseArmour) EncodeNBT() map[string]any {
	if l.Colour == (color.RGBA{}) {
		return nil
	}
	return map[string]any{"customColor": int32FromRGBA(l.Colour)}
}

// DecodeNBT restores the armour's custom dye colour.
func (l LeatherHorseArmour) DecodeNBT(data map[string]any) any {
	if v, ok := data["customColor"].(int32); ok {
		l.Colour = rgbaFromInt32(v)
	}
	return l
}

// CopperHorseArmour is a type of horse armour crafted from copper.
type CopperHorseArmour struct{ horseArmour }

// EncodeItem ...
func (CopperHorseArmour) EncodeItem() (string, int16) {
	return "minecraft:copper_horse_armor", 0
}

// IronHorseArmour is a type of horse armour offering moderate protection.
type IronHorseArmour struct{ horseArmour }

// EncodeItem ...
func (IronHorseArmour) EncodeItem() (string, int16) {
	return "minecraft:iron_horse_armor", 0
}

// GoldenHorseArmour is a type of horse armour that can be found in generated structures.
type GoldenHorseArmour struct{ horseArmour }

// EncodeItem ...
func (GoldenHorseArmour) EncodeItem() (string, int16) {
	return "minecraft:golden_horse_armor", 0
}

// DiamondHorseArmour is the most protective variant of horse armour.
type DiamondHorseArmour struct{ horseArmour }

// EncodeItem ...
func (DiamondHorseArmour) EncodeItem() (string, int16) {
	return "minecraft:diamond_horse_armor", 0
}

// NetheriteHorseArmour is the most protective horse armour, which does not
// burn.
type NetheriteHorseArmour struct{ horseArmour }

// EncodeItem ...
func (NetheriteHorseArmour) EncodeItem() (string, int16) {
	return "minecraft:netherite_horse_armor", 0
}

// FireProof ...
func (NetheriteHorseArmour) FireProof() bool { return true }

// The protection of horse armour is the body defence of its Java armour
// material.

// DefencePoints ...
func (LeatherHorseArmour) DefencePoints() float64 { return 3 }

// DefencePoints ...
func (CopperHorseArmour) DefencePoints() float64 { return 4 }

// DefencePoints ...
func (IronHorseArmour) DefencePoints() float64 { return 5 }

// DefencePoints ...
func (GoldenHorseArmour) DefencePoints() float64 { return 7 }

// DefencePoints ...
func (DiamondHorseArmour) DefencePoints() float64 { return 11 }

// Toughness ...
func (DiamondHorseArmour) Toughness() float64 { return 2 }

// DefencePoints ...
func (NetheriteHorseArmour) DefencePoints() float64 { return 19 }

// Toughness ...
func (NetheriteHorseArmour) Toughness() float64 { return 3 }

// KnockBackResistance ...
func (NetheriteHorseArmour) KnockBackResistance() float64 { return 0.1 }

// Toughness ...
func (horseArmour) Toughness() float64 { return 0 }

// KnockBackResistance ...
func (horseArmour) KnockBackResistance() float64 { return 0 }
