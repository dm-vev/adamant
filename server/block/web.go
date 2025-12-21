package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// Web is a sticky cobweb block.
type Web struct {
	empty
	transparent
	sourceWaterDisplacer
}

// EntityInside ...
func (Web) EntityInside(_ cube.Pos, _ *world.Tx, e world.Entity) {
	if fallEntity, ok := e.(fallDistanceEntity); ok {
		fallEntity.ResetFallDistance()
	}
}

// BreakInfo ...
func (w Web) BreakInfo() BreakInfo {
	return newBreakInfo(4, func(t item.Tool) bool {
		return t.ToolType() == item.TypeShears || t.ToolType() == item.TypeSword
	}, func(t item.Tool) bool {
		return t.ToolType() == item.TypeShears || t.ToolType() == item.TypeSword
	}, func(t item.Tool, enchantments []item.Enchantment) []item.Stack {
		switch t.ToolType() {
		case item.TypeShears:
			return []item.Stack{item.NewStack(w, 1)}
		case item.TypeSword:
			return []item.Stack{item.NewStack(item.String{}, 1)}
		}
		return nil
	}).withBlastResistance(20)
}

// EncodeItem ...
func (Web) EncodeItem() (name string, meta int16) {
	return "minecraft:web", 0
}

// EncodeBlock ...
func (Web) EncodeBlock() (string, map[string]any) {
	return "minecraft:web", nil
}
