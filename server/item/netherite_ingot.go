package item

import "github.com/dm-vev/adamant/net/bedrock/text"

// NetheriteIngot is a rare mineral crafted with 4 pieces of netherite scrap and 4 gold ingots.
type NetheriteIngot struct{}

// EncodeItem ...
func (NetheriteIngot) EncodeItem() (name string, meta int16) {
	return "minecraft:netherite_ingot", 0
}

// TrimMaterial ...
func (NetheriteIngot) TrimMaterial() string {
	return "netherite"
}

// MaterialColour ...
func (NetheriteIngot) MaterialColour() string {
	return text.Netherite
}

// PayableForBeacon ...
func (NetheriteIngot) PayableForBeacon() bool {
	return true
}

// FireProof reports that the item survives fire and lava, as its
// damage_resistant component for is_fire.
func (NetheriteIngot) FireProof() bool { return true }
