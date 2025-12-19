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
		{buttonBamboo}, {buttonStone}, {buttonPolishedBlackstone},
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
	}
	panic("unknown button type")
}

func (b ButtonType) Uint8() uint8 {
	return uint8(b.button)
}

func (b ButtonType) pressTicks() int {
	return 30
}
