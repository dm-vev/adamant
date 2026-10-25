package block

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/loot"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Leaves are blocks that grow as part of trees which mainly drop saplings and sticks.
type Leaves struct {
	leaves
	sourceWaterDisplacer

	// Type is the type of the leaves.
	Type LeavesType
	// Persistent specifies if the leaves are persistent, meaning they will not decay as a result of no wood
	// being nearby.
	Persistent bool

	ShouldUpdate bool
}

// UseOnBlock makes leaves persistent when they are placed so that they don't decay.
func (l Leaves) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) (used bool) {
	pos, _, used = firstReplaceable(tx, pos, face, l)
	if !used {
		return
	}
	l.Persistent = true

	place(tx, pos, l, user, ctx)
	return placed(ctx)
}

// leavesNearLog ports the DISTANCE of LeavesBlock: a leaf stays while a
// block of prevents_nearby_leaf_decay, a log of any kind, lies within six
// steps of it through leaves.
func leavesNearLog(pos cube.Pos, tx *world.Tx) bool {
	type step struct {
		pos      cube.Pos
		distance int
	}
	visited := map[cube.Pos]struct{}{pos: {}}
	queue := []step{{pos, 0}}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		for _, face := range cube.Faces() {
			next := n.pos.Side(face)
			if _, seen := visited[next]; seen || next.OutOfBounds(tx.Range()) {
				continue
			}
			visited[next] = struct{}{}
			if javaBlockTag(tx, next, "minecraft:prevents_nearby_leaf_decay") {
				return true
			}
			if _, leaves := tx.Block(next).(Leaves); leaves && n.distance+1 < 6 {
				queue = append(queue, step{next, n.distance + 1})
			}
		}
	}
	return false
}

// leavesUpdateBitOpts sets only a leaf's update_bit. Like BDS, flagging or
// clearing it does not notify the neighbours: otherwise every leaf of a tree
// would keep flagging the next one.
var leavesUpdateBitOpts = &world.SetOpts{DisableBlockUpdates: true}

// RandomTick decays a flagged leaf that no longer reaches a log: it drops its
// loot and is removed, as LeavesBlock.randomTick does.
func (l Leaves) RandomTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	if !l.Persistent && l.ShouldUpdate {
		if leavesNearLog(pos, tx) {
			l.ShouldUpdate = false
			tx.SetBlock(pos, l, leavesUpdateBitOpts)
			return
		}
		ctx := tx.Event()
		if tx.World().Handler().HandleLeavesDecay(ctx, pos); ctx.Cancelled() {
			// Prevent immediate re-updating.
			l.ShouldUpdate = false
			tx.SetBlock(pos, l, leavesUpdateBitOpts)
			return
		}
		for _, drop := range leavesDrops(l, item.ToolNone{}, nil, tx, pos) {
			dropItem(tx, drop, pos.Vec3Centre())
		}
		tx.SetBlock(pos, nil, nil)
	}
}

// NeighbourUpdateTick ...
func (l Leaves) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !l.Persistent && !l.ShouldUpdate {
		l.ShouldUpdate = true
		tx.SetBlock(pos, l, leavesUpdateBitOpts)
	}
}

// FlammabilityInfo ...
func (l Leaves) FlammabilityInfo() FlammabilityInfo {
	return newFlammabilityInfo(30, 60, true)
}

// BreakInfo ...
func (l Leaves) BreakInfo() BreakInfo {
	return newBreakInfo(0.2, alwaysHarvestable, func(t item.Tool) bool {
		return t.ToolType() == item.TypeShears || t.ToolType() == item.TypeHoe
	}, func(t item.Tool, enchantments []item.Enchantment) []item.Stack {
		return leavesDrops(l, t, enchantments, nil, cube.Pos{})
	})
}

func leavesDrops(l Leaves, tool item.Tool, enchantments []item.Enchantment, tx *world.Tx, pos cube.Pos) []item.Stack {
	stack, ok := tool.(world.Item)
	if !ok {
		stack = l
	}
	toolStack := item.NewStack(stack, 1).WithForcedEnchantments(enchantments...)
	table := "minecraft:blocks/" + l.Type.String()
	context := loot.Context{
		Tool: stack, ToolStack: toolStack, Block: "minecraft:" + l.Type.String(),
		BlockState: l, Origin: pos.Vec3Centre(), Tx: tx,
	}
	if tx != nil {
		context.RandomSequence = tx.LootRandomSequence
	}
	drops, err := loot.Generate(table, rand.Int64(), context)
	if err != nil {
		panic(err)
	}
	return drops
}

// CompostChance ...
func (Leaves) CompostChance() float64 {
	return 0.3
}

// EncodeItem ...
func (l Leaves) EncodeItem() (name string, meta int16) {
	return "minecraft:" + l.Type.String(), 0
}

// LightDiffusionLevel ...
func (Leaves) LightDiffusionLevel() uint8 {
	return 1
}

// SideClosed ...
func (Leaves) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool {
	return false
}

// EncodeBlock ...
func (l Leaves) EncodeBlock() (name string, properties map[string]any) {
	return "minecraft:" + l.Type.String(), map[string]any{"persistent_bit": l.Persistent, "update_bit": l.ShouldUpdate}
}

// allLeaves returns a list of all possible leaves states.
func allLeaves() (leaves []world.Block) {
	f := func(persistent, update bool) {
		for _, t := range LeavesTypes() {
			leaves = append(leaves, Leaves{Type: t, Persistent: persistent, ShouldUpdate: update})
		}
	}
	f(true, true)
	f(true, false)
	f(false, true)
	f(false, false)
	return
}
