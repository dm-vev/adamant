package item

import "github.com/df-mc/dragonfly/server/text"

type RedstoneWire struct{}

// EncodeItem ...
func (RedstoneWire) EncodeItem() (name string, meta int16) {
	return "minecraft:redstone", 0
}

// TrimMaterial ...
func (RedstoneWire) TrimMaterial() string {
	return "redstone"
}

// MaterialColour ...
func (RedstoneWire) MaterialColour() text.Colour {
	return text.Redstone
}
