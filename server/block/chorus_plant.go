package block

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// ChorusPlant is a block found growing in the End. It drops chorus fruit when broken.
type ChorusPlant struct {
	transparent
}

// Model ...
func (ChorusPlant) Model() world.BlockModel {
	return model.ChorusPlant{}
}

// BreakInfo ...
func (ChorusPlant) BreakInfo() BreakInfo {
	return newBreakInfo(0.4, alwaysHarvestable, nothingEffective, func(item.Tool, []item.Enchantment) []item.Stack {
		if amount := rand.IntN(2); amount != 0 {
			return []item.Stack{item.NewStack(item.ChorusFruit{}, amount)}
		}
		return nil
	})
}

// NeighbourUpdateTick ...
func (c ChorusPlant) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !c.canSurviveAt(pos, tx) {
		breakBlock(c, pos, tx)
	}
}

// HasLiquidDrops ...
func (ChorusPlant) HasLiquidDrops() bool {
	return true
}

// FlammabilityInfo ...
func (ChorusPlant) FlammabilityInfo() FlammabilityInfo {
	return newFlammabilityInfo(60, 100, false)
}

// CompostChance ...
func (ChorusPlant) CompostChance() float64 {
	return 0.65
}

// UseOnBlock ...
func (c ChorusPlant) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, c)
	if !used || !c.canSurviveAt(pos, tx) {
		return false
	}
	place(tx, pos, c, user, ctx)
	return placed(ctx)
}

// EncodeItem ...
func (ChorusPlant) EncodeItem() (name string, meta int16) {
	return "minecraft:chorus_plant", 0
}

// EncodeBlock ...
func (ChorusPlant) EncodeBlock() (name string, properties map[string]any) {
	return "minecraft:chorus_plant", nil
}

func (ChorusPlant) canSurviveAt(pos cube.Pos, tx *world.Tx) bool {
	_, upAir := tx.Block(pos.Side(cube.FaceUp)).(Air)
	_, downAir := tx.Block(pos.Side(cube.FaceDown)).(Air)

	for _, face := range cube.HorizontalFaces() {
		side := pos.Side(face)
		if _, ok := tx.Block(side).(ChorusPlant); !ok {
			continue
		}
		if !upAir && !downAir {
			return false
		}
		switch tx.Block(side.Side(cube.FaceDown)).(type) {
		case ChorusPlant, EndStone:
			return true
		}
	}

	switch tx.Block(pos.Side(cube.FaceDown)).(type) {
	case ChorusPlant, EndStone:
		return true
	}
	return false
}
