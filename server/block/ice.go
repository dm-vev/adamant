package block

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

var hashIce = NextHash()

// Ice is a transparent solid block that is slippery.
type Ice struct {
	solid
	transparent
}

// BreakInfo ports IceBlock: ice drops only to Silk Touch, and ice a player
// breaks without it leaves water over a block that blocks motion or a
// liquid, or nothing where water evaporates.
func (i Ice) BreakInfo() BreakInfo {
	return newBreakInfo(0.5, alwaysHarvestable, pickaxeEffective, silkTouchOnlyDrop(i)).withBreakHandler(func(pos cube.Pos, tx *world.Tx, u item.User) {
		if mode, ok := u.(interface{ GameMode() world.GameMode }); ok && mode.GameMode().CreativeInventory() {
			return
		}
		if held, _ := u.HeldItems(); hasSilkTouch(held.Enchantments()) {
			return
		}
		if tx.World().Dimension().WaterEvaporates() {
			return
		}
		below := pos.Side(cube.FaceDown)
		_, liquid := tx.Liquid(below)
		if liquid || len(tx.Block(below).Model().BBox(below, tx)) > 0 {
			tx.SetBlock(pos, Water{Depth: 8, Still: true}, nil)
		}
	})
}

// RandomTick ports IceBlock.randomTick: ice melts where block light is above
// 10, eleven less the light it dampens.
func (i Ice) RandomTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	if tx.BlockLight(pos) > 10 {
		i.melt(pos, tx)
	}
}

// melt ports IceBlock.melt: ice turns into water, or vanishes where water
// evaporates.
func (Ice) melt(pos cube.Pos, tx *world.Tx) {
	if tx.World().Dimension().WaterEvaporates() {
		tx.SetBlock(pos, nil, nil)
		return
	}
	tx.SetBlock(pos, Water{Depth: 8, Still: true}, nil)
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
