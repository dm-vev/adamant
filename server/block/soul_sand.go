package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
)

// SoulSand is a block found naturally only in the Nether. SoulSand slows movement of mobs & players.
type SoulSand struct {
	solid
}

// soulSandBoxes is the collision shape of soul sand: 14 pixels high, as Java's SoulSandBlock collides. A
// player stands 2 pixels lower on it than on a full block, as a Bedrock client does too.
var soulSandBoxes = []cube.BBox{cube.Box(0, 0, 0, 1, 14.0/16, 1)}

// soulSandModel is the model of soul sand: solid on every side, but 14 pixels high to collide with.
type soulSandModel struct{ model.Solid }

// BBox returns the collision shape of soul sand.
func (soulSandModel) BBox(cube.Pos, world.BlockSource) []cube.BBox { return soulSandBoxes }

// Model returns the model of soul sand.
func (SoulSand) Model() world.BlockModel { return soulSandModel{} }

// NeighbourUpdateTick updates a bubble column above the soul sand when it or the block above changes.
func (SoulSand) NeighbourUpdateTick(pos, changedNeighbour cube.Pos, tx *world.Tx) {
	if changedNeighbour == pos || changedNeighbour == pos.Side(cube.FaceUp) {
		updateBubbleColumn(pos.Side(cube.FaceUp), tx)
	}
}

// SoilFor ...
func (s SoulSand) SoilFor(block world.Block) bool {
	flower, ok := block.(Flower)
	return ok && flower.Type == WitherRose()
}

// Instrument ...
func (s SoulSand) Instrument() sound.Instrument {
	return sound.CowBell()
}

// BreakInfo ...
func (s SoulSand) BreakInfo() BreakInfo {
	return newBreakInfo(0.5, alwaysHarvestable, shovelEffective, oneOf(s))
}

// EncodeItem ...
func (SoulSand) EncodeItem() (name string, meta int16) {
	return "minecraft:soul_sand", 0
}

// EncodeBlock ...
func (SoulSand) EncodeBlock() (string, map[string]any) {
	return "minecraft:soul_sand", nil
}
