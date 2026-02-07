package block

// TorchColour is the colour of a coloured torch.
type TorchColour struct {
	colouredTorch
}

type colouredTorch uint8

// TorchColourPurple returns the purple torch colour.
func TorchColourPurple() TorchColour {
	return TorchColour{0}
}

// TorchColourBlue returns the blue torch colour.
func TorchColourBlue() TorchColour {
	return TorchColour{1}
}

// TorchColourGreen returns the green torch colour.
func TorchColourGreen() TorchColour {
	return TorchColour{2}
}

// TorchColourRed returns the red torch colour.
func TorchColourRed() TorchColour {
	return TorchColour{3}
}

// Name returns the human-readable name of the torch colour.
func (c colouredTorch) Name() string {
	switch c {
	case 0:
		return "Purple Torch"
	case 1:
		return "Blue Torch"
	case 2:
		return "Green Torch"
	case 3:
		return "Red Torch"
	}
	panic("unknown torch colour")
}

// String returns the torch colour as a string.
func (c colouredTorch) String() string {
	switch c {
	case 0:
		return "purple"
	case 1:
		return "blue"
	case 2:
		return "green"
	case 3:
		return "red"
	}
	panic("unknown torch colour")
}

// Uint8 returns the torch colour as a uint8.
func (c colouredTorch) Uint8() uint8 {
	return uint8(c)
}

// TorchColours returns all possible torch colours.
func TorchColours() []TorchColour {
	return []TorchColour{TorchColourPurple(), TorchColourBlue(), TorchColourGreen(), TorchColourRed()}
}
