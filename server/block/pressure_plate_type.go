package block

// PressurePlateType represents a type of pressure plate material.
type PressurePlateType struct {
	plate pressurePlateType
}

type pressurePlateType uint8

const (
	plateWood pressurePlateType = iota
	plateSpruce
	plateBirch
	plateJungle
	plateAcacia
	plateDarkOak
	plateCrimson
	plateWarped
	plateMangrove
	plateCherry
	platePaleOak
	plateBamboo
	plateStone
	platePolishedBlackstone
	plateLightWeighted
	plateHeavyWeighted
)

// PressurePlateTypes returns all supported pressure plate types.
func PressurePlateTypes() []PressurePlateType {
	return []PressurePlateType{
		{plateWood}, {plateSpruce}, {plateBirch}, {plateJungle}, {plateAcacia}, {plateDarkOak},
		{plateCrimson}, {plateWarped}, {plateMangrove}, {plateCherry}, {platePaleOak}, {plateBamboo},
		{plateStone}, {platePolishedBlackstone}, {plateLightWeighted}, {plateHeavyWeighted},
	}
}

func (p PressurePlateType) blockName() string {
	switch p.plate {
	case plateWood:
		return "wooden_pressure_plate"
	case plateSpruce:
		return "spruce_pressure_plate"
	case plateBirch:
		return "birch_pressure_plate"
	case plateJungle:
		return "jungle_pressure_plate"
	case plateAcacia:
		return "acacia_pressure_plate"
	case plateDarkOak:
		return "dark_oak_pressure_plate"
	case plateCrimson:
		return "crimson_pressure_plate"
	case plateWarped:
		return "warped_pressure_plate"
	case plateMangrove:
		return "mangrove_pressure_plate"
	case plateCherry:
		return "cherry_pressure_plate"
	case platePaleOak:
		return "pale_oak_pressure_plate"
	case plateBamboo:
		return "bamboo_pressure_plate"
	case plateStone:
		return "stone_pressure_plate"
	case platePolishedBlackstone:
		return "polished_blackstone_pressure_plate"
	case plateLightWeighted:
		return "light_weighted_pressure_plate"
	case plateHeavyWeighted:
		return "heavy_weighted_pressure_plate"
	}
	panic("unknown pressure plate type")
}

func (p PressurePlateType) Uint8() uint8 {
	return uint8(p.plate)
}

func (p PressurePlateType) weighted() bool {
	return p.plate == plateLightWeighted || p.plate == plateHeavyWeighted
}

func (p PressurePlateType) maxWeight() int {
	switch p.plate {
	case plateLightWeighted:
		return 15
	case plateHeavyWeighted:
		return 150
	}
	return 0
}

func (p PressurePlateType) triggersItems() bool {
	switch p.plate {
	case plateStone, platePolishedBlackstone:
		return false
	}
	return true
}
