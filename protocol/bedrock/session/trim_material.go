package session

import (
	bedrocktext "github.com/df-mc/dragonfly/protocol/bedrock/text"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/text"
)

// trimMaterialColour returns the colour of a trim material as TrimData sends
// it: the § code of the colour.
func trimMaterialColour(m item.ArmourTrimMaterial) string {
	return bedrocktext.Code(text.Code{Colour: m.MaterialColour()})
}
