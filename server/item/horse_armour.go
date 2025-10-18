package item

// horseArmour is a shared implementation for the various horse armour items.
type horseArmour struct{}

// MaxCount always returns 1.
func (horseArmour) MaxCount() int {
	return 1
}

// LeatherHorseArmour is a type of horse armour that offers minimal protection.
type LeatherHorseArmour struct{ horseArmour }

// EncodeItem ...
func (LeatherHorseArmour) EncodeItem() (string, int16) {
	return "minecraft:leather_horse_armor", 0
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
