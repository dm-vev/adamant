package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

var hashTrappedChest = NextHash()

// TrappedChest is a chest that emits a redstone signal equal to the number
// of players viewing it (Java TrappedChestBlock), strongly powering the block
// below it. It only pairs with other trapped chests.
type TrappedChest struct {
	Chest
}

// NewTrappedChest creates a trapped chest with an initialised inventory.
func NewTrappedChest() TrappedChest { return TrappedChest{Chest: NewChest()} }

// EncodeItem ...
func (TrappedChest) EncodeItem() (string, int16) { return "minecraft:trapped_chest", 0 }

// EncodeBlock ...
func (c TrappedChest) EncodeBlock() (string, map[string]any) {
	return "minecraft:trapped_chest", map[string]any{"minecraft:cardinal_direction": c.Facing.String()}
}

// Hash ...
func (c TrappedChest) Hash() (uint64, uint64) { return hashTrappedChest, uint64(c.Facing) }

// DecodeNBT ...
func (c TrappedChest) DecodeNBT(m map[string]any) any {
	c.Chest = c.Chest.DecodeNBT(m).(Chest)
	return c
}

func (c TrappedChest) withChestData(data Chest) chestBlock { c.Chest = data; return c }
func (TrappedChest) canPairChest(other world.Block) bool   { _, ok := other.(TrappedChest); return ok }

// WithJavaChestState preserves contents across pairing updates.
func (c TrappedChest) WithJavaChestState(previous world.Block, pos cube.Pos, kind string) world.Block {
	c.Chest = c.Chest.withJavaChestState(previous, pos, kind)
	return c
}

// UseOnBlock places the trapped chest, pairing it with a trapped chest beside
// it that faces the same way.
func (c TrappedChest) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, c)
	if !used {
		return false
	}
	c = NewTrappedChest()
	c.Facing = user.Rotation().Direction().Opposite()
	for _, dir := range []cube.Direction{c.Facing.RotateLeft(), c.Facing.RotateRight()} {
		pairPos := pos.Side(dir.Face())
		if ch, pair, ok := c.pair(tx, pos, pairPos, c); ok {
			place(tx, pos, ch, user, ctx)
			tx.SetBlock(pairPos, pair, nil)
			return placed(ctx)
		}
	}
	place(tx, pos, c, user, ctx)
	return placed(ctx)
}

// AddViewer opens the chest and updates the redstone signal.
func (c TrappedChest) AddViewer(v ContainerViewer, tx *world.Tx, pos cube.Pos) {
	c.Chest.AddViewer(v, tx, pos)
	c.updateSignal(tx, pos)
}

// RemoveViewer closes the chest for the viewer and updates the redstone
// signal.
func (c TrappedChest) RemoveViewer(v ContainerViewer, tx *world.Tx, pos cube.Pos) {
	c.Chest.RemoveViewer(v, tx, pos)
	c.updateSignal(tx, pos)
}

// updateSignal ports TrappedChestBlockEntity.signalOpenCount: neighbours and
// the strongly powered block below are re-evaluated, for both halves of a
// double chest.
func (c TrappedChest) updateSignal(tx *world.Tx, pos cube.Pos) {
	positions := []cube.Pos{pos}
	if c.paired {
		positions = append(positions, c.pairPos(pos))
	}
	for _, p := range positions {
		tx.Redstone().ScheduleUpdate(p)
		tx.Redstone().ScheduleUpdate(p.Side(cube.FaceDown))
	}
}

// openCount returns how many viewers have the chest open.
func (c TrappedChest) openCount() int {
	if c.viewerMu == nil {
		return 0
	}
	c.viewerMu.RLock()
	defer c.viewerMu.RUnlock()
	return min(len(c.viewers), 15)
}

// RedstonePower returns the number of viewers, capped at 15.
func (c TrappedChest) RedstonePower(cube.Pos, *world.Tx, cube.Face) int { return c.openCount() }

// RedstoneStrongPower strongly powers only the block below the chest.
func (c TrappedChest) RedstoneStrongPower(_ cube.Pos, _ *world.Tx, face cube.Face) int {
	if face != cube.FaceDown {
		return 0
	}
	return c.openCount()
}

// BreakInfo ...
func (c TrappedChest) BreakInfo() BreakInfo {
	return newBreakInfo(2.5, alwaysHarvestable, axeEffective, oneOf(TrappedChest{})).withBreakHandler(func(pos cube.Pos, tx *world.Tx, _ item.User) {
		c.Chest.dropContents(pos, tx, c)
	})
}

func allTrappedChests() (chests []world.Block) {
	for _, facing := range []cube.Direction{cube.North, cube.East, cube.South, cube.West} {
		c := NewTrappedChest()
		c.Facing = facing
		chests = append(chests, c)
	}
	return
}
