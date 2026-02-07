package block

import "github.com/df-mc/dragonfly/server/world"

// ColoredTorch is an alias for ColouredTorch.
type ColoredTorch = ColouredTorch

// ColoredTorchColour is an alias for TorchColour.
type ColoredTorchColour = TorchColour

// BlueTorch returns the blue torch colour.
func BlueTorch() ColoredTorchColour {
	return TorchColourBlue()
}

// GreenTorch returns the green torch colour.
func GreenTorch() ColoredTorchColour {
	return TorchColourGreen()
}

// PurpleTorch returns the purple torch colour.
func PurpleTorch() ColoredTorchColour {
	return TorchColourPurple()
}

// RedTorch returns the red torch colour.
func RedTorch() ColoredTorchColour {
	return TorchColourRed()
}

func allColoredTorches() []world.Block {
	return allColouredTorches()
}
