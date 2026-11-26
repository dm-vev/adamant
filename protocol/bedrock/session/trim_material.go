package session

import (
	bedrocktext "github.com/dm-vev/adamant/protocol/bedrock/text"
	"github.com/dm-vev/adamant/server/item"
	"github.com/dm-vev/adamant/server/text"
)

// trimMaterialColour returns the colour of a trim material as TrimData sends
// it: the § code of the colour.
func trimMaterialColour(m item.ArmourTrimMaterial) string {
	return bedrocktext.Code(text.Code{Colour: m.MaterialColour()})
}
