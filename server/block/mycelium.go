package block

import "github.com/df-mc/dragonfly/server/world"

// Mycelium is a dirt-type block that naturally blankets the surface of mushroom islands.
type Mycelium struct {
	solid
}

var myceliumHash = NextHash()

// Hash ...
func (Mycelium) Hash() (uint64, uint64) {
	return myceliumHash, 0
}

// SoilFor ...
func (m Mycelium) SoilFor(block world.Block) bool {
	switch block.(type) {
	case ShortGrass, Fern, DoubleTallGrass, Flower, DoubleFlower, NetherSprouts, DeadBush, SugarCane:
		return true
	}
	return false
}

// Shovel ...
func (Mycelium) Shovel() (world.Block, bool) {
	return DirtPath{}, true
}

// BreakInfo ...
func (m Mycelium) BreakInfo() BreakInfo {
	return newBreakInfo(0.6, alwaysHarvestable, shovelEffective, silkTouchOneOf(Dirt{}, m))
}

// EncodeItem ...
func (Mycelium) EncodeItem() (name string, meta int16) {
	return "minecraft:mycelium", 0
}

// EncodeBlock ...
func (Mycelium) EncodeBlock() (string, map[string]any) {
	return "minecraft:mycelium", nil
}
