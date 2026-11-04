package block

// PetrifiedOak is a marker block used for petrified oak slabs.
type PetrifiedOak struct {
	solid
}

// BreakInfo ...
func (p PetrifiedOak) BreakInfo() BreakInfo {
	return newBreakInfo(2, pickaxeHarvestable, pickaxeEffective, oneOf(p))
}

// EncodeItem ...
func (PetrifiedOak) EncodeItem() (name string, meta int16) {
	return "minecraft:petrified_oak_slab", 0
}

// EncodeBlock ...
func (PetrifiedOak) EncodeBlock() (string, map[string]any) {
	return "minecraft:petrified_oak", nil
}
