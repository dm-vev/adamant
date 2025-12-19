package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// RedstoneBlock is a solid block that emits constant redstone power.
type RedstoneBlock struct {
	solid
}

// BreakInfo ...
func (r RedstoneBlock) BreakInfo() BreakInfo {
	return newBreakInfo(5, pickaxeHarvestable, pickaxeEffective, oneOf(r))
}

// RedstoneWeakPower ...
func (RedstoneBlock) RedstoneWeakPower(cube.Face) uint8 {
	return 15
}

// RedstoneStrongPower ...
func (RedstoneBlock) RedstoneStrongPower(cube.Face) uint8 {
	return 15
}

// EncodeItem ...
func (RedstoneBlock) EncodeItem() (name string, meta int16) {
	return "minecraft:redstone_block", 0
}

// EncodeBlock ...
func (RedstoneBlock) EncodeBlock() (string, map[string]any) {
	return "minecraft:redstone_block", nil
}

func allRedstoneBlocks() []world.Block {
	return []world.Block{RedstoneBlock{}}
}
