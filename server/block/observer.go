package block

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Observer detects block updates and emits a redstone pulse.
type Observer struct {
	solid

	Facing  cube.Face
	Powered bool
}

// BreakInfo ...
func (o Observer) BreakInfo() BreakInfo {
	return newBreakInfo(3, alwaysHarvestable, pickaxeEffective, oneOf(o))
}

// Removed cancels the old observer's queued pulse, as ObserverBlock::onRemove
// does. A permutation change of the same observer retains its native queue.
func (o Observer) Removed(pos cube.Pos, tx *world.Tx, replacement world.Block) {
	if _, same := replacement.(Observer); !same {
		tx.CancelBlockUpdate(pos, o)
	}
}

// UseOnBlock ...
func (o Observer) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, o)
	if !used {
		return false
	}
	o.Facing = placerFacing(user, pos).Opposite()
	o.Powered = false
	place(tx, pos, o, user, ctx)
	return placed(ctx)
}

// NeighbourUpdateTick ...
func (o Observer) NeighbourUpdateTick(pos, changedNeighbour cube.Pos, tx *world.Tx) {
	o.NeighbourChanged(pos, changedNeighbour, tx)
}

// NeighbourChanged observes the powered state when the notification occurs.
// Deferring this check can turn an ignored change during a pulse into a new
// pulse if the pending turn-off runs before the deferred neighbor callback.
func (o Observer) NeighbourChanged(pos, changedNeighbour cube.Pos, tx *world.Tx) {
	if changedNeighbour != pos.Side(o.Facing) {
		return
	}
	if o.Powered {
		return
	}
	if tx.HasScheduledBlockUpdate(pos, o) {
		return
	}
	tx.ScheduleBlockUpdate(pos, o, gameTicks(2))
}

// ScheduledTick ...
func (o Observer) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	// ObserverBlock::tick reschedules for the next game tick when the
	// dimension's shared circuit step has not arrived yet.
	if tx.CurrentTick()&1 != 0 {
		tx.ScheduleBlockUpdate(pos, o, gameTicks(1))
		return
	}
	if o.Powered {
		o.Powered = false
		tx.SetBlock(pos, o, nil)
		tx.Redstone().SetCircuitStrength(pos, 0)
		tx.DoBlockUpdatesAround(pos.Side(o.Facing.Opposite()))
		return
	}
	o.Powered = true
	tx.SetBlock(pos, o, nil)
	tx.Redstone().SetCircuitStrength(pos, 15)
	tx.DoBlockUpdatesAround(pos.Side(o.Facing.Opposite()))
	tx.ScheduleBlockUpdate(pos, o, gameTicks(2))
}

func (o Observer) RedstonePower(pos cube.Pos, tx *world.Tx, face cube.Face) int {
	if face == o.Facing.Opposite() && tx != nil {
		return tx.Redstone().CircuitStrength(pos)
	}
	return 0
}

// MovedByPiston runs the native observer move callback: every completed push
// or pull starts a pulse, regardless of which face changed around the block.
func (o Observer) MovedByPiston(pos cube.Pos, tx *world.Tx) {
	o.Powered = true
	tx.SetBlock(pos, o, nil)
	tx.Redstone().SetCircuitStrength(pos, 15)
	tx.ScheduleBlockUpdate(pos, o, gameTicks(2))
}

func (o Observer) RedstoneStrongPower(pos cube.Pos, tx *world.Tx, face cube.Face) int {
	return o.RedstonePower(pos, tx, face)
}

// EncodeItem ...
func (Observer) EncodeItem() (name string, meta int16) {
	return "minecraft:observer", 0
}

// EncodeBlock ...
func (o Observer) EncodeBlock() (string, map[string]any) {
	return "minecraft:observer", map[string]any{
		"minecraft:facing_direction": o.Facing.String(),
		"powered_bit":                boolByte(o.Powered),
	}
}

func allObservers() (observers []world.Block) {
	for _, face := range cube.Faces() {
		for _, powered := range []bool{false, true} {
			observers = append(observers, Observer{Facing: face, Powered: powered})
		}
	}
	return
}
