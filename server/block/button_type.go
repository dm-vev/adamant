package block

// ButtonType represents a type of button material.
type ButtonType struct {
	button buttonType
}

type buttonType uint8

const (
	buttonOak buttonType = iota
	buttonSpruce
	buttonBirch
	buttonJungle
	buttonAcacia
	buttonDarkOak
	buttonCrimson
	buttonWarped
	buttonMangrove
	buttonCherry
	buttonPaleOak
	buttonBamboo
	buttonStone
	buttonPolishedBlackstone
	buttonPoplar
)

// WoodButton returns a wooden button type (oak).
func WoodButton() ButtonType { return ButtonType{buttonOak} }

// StoneButton returns a stone button type.
func StoneButton() ButtonType { return ButtonType{buttonStone} }

// ButtonTypes returns all supported button types.
func ButtonTypes() []ButtonType {
	return []ButtonType{
		{buttonOak}, {buttonSpruce}, {buttonBirch}, {buttonJungle}, {buttonAcacia}, {buttonDarkOak},
		{buttonCrimson}, {buttonWarped}, {buttonMangrove}, {buttonCherry}, {buttonPaleOak},
		{buttonBamboo}, {buttonPoplar}, {buttonStone}, {buttonPolishedBlackstone},
	}
}

func (b ButtonType) blockName() string {
	switch b.button {
	case buttonOak:
		return "wooden_button"
	case buttonSpruce:
		return "spruce_button"
	case buttonBirch:
		return "birch_button"
	case buttonJungle:
		return "jungle_button"
	case buttonAcacia:
		return "acacia_button"
	case buttonDarkOak:
		return "dark_oak_button"
	case buttonCrimson:
		return "crimson_button"
	case buttonWarped:
		return "warped_button"
	case buttonMangrove:
		return "mangrove_button"
	case buttonCherry:
		return "cherry_button"
	case buttonPaleOak:
		return "pale_oak_button"
	case buttonBamboo:
		return "bamboo_button"
	case buttonStone:
		return "stone_button"
	case buttonPolishedBlackstone:
		return "polished_blackstone_button"
	case buttonPoplar:
		return "poplar_button"
	}
	panic("unknown button type")
}

func (b ButtonType) Uint8() uint8 {
	return uint8(b.button)
}

// pressTicks ports BlockSetType.ticksToStayPressed: 20 ticks for stone
// buttons and 30 for wooden ones.
func (b ButtonType) pressTicks() int {
	if b.button == buttonStone || b.button == buttonPolishedBlackstone {
		return 20
	}
	return 30
}
