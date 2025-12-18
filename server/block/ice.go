package block

var hashIce = NextHash()

// Ice is a transparent solid block that is slippery.
type Ice struct {
	solid
	transparent
}

// BreakInfo ...
func (i Ice) BreakInfo() BreakInfo {
	return newBreakInfo(0.5, alwaysHarvestable, pickaxeEffective, oneOf(i))
}

// Friction ...
func (Ice) Friction() float64 {
	return 0.98
}

// EncodeItem ...
func (Ice) EncodeItem() (name string, meta int16) {
	return "minecraft:ice", 0
}

// EncodeBlock ...
func (Ice) EncodeBlock() (string, map[string]any) {
	return "minecraft:ice", nil
}

// Hash ...
func (Ice) Hash() (uint64, uint64) {
	return hashIce, 0
}
