package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/event"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"math/rand/v2"
)

// AzaleaLeaves are decorative leaves used by azalea trees.
type AzaleaLeaves struct {
	leaves
	sourceWaterDisplacer

	// Persistent specifies if the leaves are persistent, meaning they will not decay as a result of no wood
	// being nearby.
	Persistent bool
	// ShouldUpdate specifies if the leaves should check for decay.
	ShouldUpdate bool
	// Flowered specifies if these leaves are flowered.
	Flowered bool
}

// UseOnBlock makes leaves persistent when they are placed so that they don't decay.
func (l AzaleaLeaves) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) (used bool) {
	pos, _, used = firstReplaceable(tx, pos, face, l)
	if !used {
		return
	}
	l.Persistent = true

	place(tx, pos, l, user, ctx)
	return placed(ctx)
}

// RandomTick ...
func (l AzaleaLeaves) RandomTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	if !l.Persistent && l.ShouldUpdate {
		if findLog(pos, tx, &[]cube.Pos{}, 0) {
			l.ShouldUpdate = false
			tx.SetBlock(pos, l, nil)
			return
		}
		ctx := event.C(tx)
		if tx.World().Handler().HandleLeavesDecay(ctx, pos); ctx.Cancelled() {
			// Prevent immediate re-updating.
			l.ShouldUpdate = false
			tx.SetBlock(pos, l, nil)
			return
		}
		tx.SetBlock(pos, nil, nil)
		for _, drop := range l.BreakInfo().Drops(item.ToolNone{}, nil) {
			dropItem(tx, drop, pos.Vec3Centre())
		}
	}
}

// NeighbourUpdateTick ...
func (l AzaleaLeaves) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !l.Persistent && !l.ShouldUpdate {
		l.ShouldUpdate = true
		tx.SetBlock(pos, l, nil)
	}
}

// FlammabilityInfo ...
func (AzaleaLeaves) FlammabilityInfo() FlammabilityInfo {
	return newFlammabilityInfo(30, 60, true)
}

// BreakInfo ...
func (l AzaleaLeaves) BreakInfo() BreakInfo {
	return newBreakInfo(0.2, alwaysHarvestable, func(t item.Tool) bool {
		return t.ToolType() == item.TypeShears || t.ToolType() == item.TypeHoe
	}, func(t item.Tool, enchantments []item.Enchantment) []item.Stack {
		if t.ToolType() == item.TypeShears || hasSilkTouch(enchantments) {
			return []item.Stack{item.NewStack(l, 1)}
		}
		if rand.Float64() < 0.02 {
			return []item.Stack{item.NewStack(item.Stick{}, rand.IntN(2)+1)}
		}
		return nil
	})
}

// CompostChance ...
func (AzaleaLeaves) CompostChance() float64 {
	return 0.3
}

// EncodeItem ...
func (l AzaleaLeaves) EncodeItem() (name string, meta int16) {
	if l.Flowered {
		return "minecraft:azalea_leaves_flowered", 0
	}
	return "minecraft:azalea_leaves", 0
}

// EncodeBlock ...
func (l AzaleaLeaves) EncodeBlock() (name string, properties map[string]any) {
	if l.Flowered {
		return "minecraft:azalea_leaves_flowered", map[string]any{"persistent_bit": l.Persistent, "update_bit": l.ShouldUpdate}
	}
	return "minecraft:azalea_leaves", map[string]any{"persistent_bit": l.Persistent, "update_bit": l.ShouldUpdate}
}

// allAzaleaLeaves returns all possible azalea leaf states.
func allAzaleaLeaves() (leaves []world.Block) {
	f := func(persistent, update, flowered bool) {
		leaves = append(leaves, AzaleaLeaves{Persistent: persistent, ShouldUpdate: update, Flowered: flowered})
	}
	f(true, true, false)
	f(true, false, false)
	f(false, true, false)
	f(false, false, false)
	f(true, true, true)
	f(true, false, true)
	f(false, true, true)
	f(false, false, true)
	return
}
