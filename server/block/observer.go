package block

import (
	"math/rand/v2"
	"sync"

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

type observerPendingPulse struct {
	pos  cube.Pos
	tick int64
}

var (
	observerPendingMu sync.Mutex
	observerPending   = map[*world.World][]observerPendingPulse{}
)

// BreakInfo ...
func (o Observer) BreakInfo() BreakInfo {
	return newBreakInfo(3, alwaysHarvestable, pickaxeEffective, oneOf(o))
}

// UseOnBlock ...
func (o Observer) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, o)
	if !used {
		return false
	}
	o.Facing = calculateFace(user, pos).Opposite()
	o.Powered = false
	place(tx, pos, o, user, ctx)
	return placed(ctx)
}

// NeighbourUpdateTick ...
func (o Observer) NeighbourUpdateTick(pos, changedNeighbour cube.Pos, tx *world.Tx) {
	if changedNeighbour != pos.Side(o.Facing) {
		return
	}
	if o.Powered {
		return
	}
	if observerPulsePending(tx.World(), pos) {
		return
	}
	tx.ScheduleBlockUpdate(pos, o, redstoneTicks(1))
}

// ScheduledTick ...
func (o Observer) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	if o.Powered {
		o.Powered = false
		tx.SetBlock(pos, o, nil)
		tx.DoBlockUpdatesAround(pos.Side(o.Facing.Opposite()))
		return
	}
	observerClearPending(tx.World(), pos)
	o.Powered = true
	tx.SetBlock(pos, o, nil)
	tx.DoBlockUpdatesAround(pos.Side(o.Facing.Opposite()))
	tx.ScheduleBlockUpdate(pos, o, redstoneTicks(2))
}

// RedstoneWeakPower ...
func (o Observer) RedstoneWeakPower(face cube.Face) uint8 {
	if o.Powered && face == o.Facing.Opposite() {
		return 15
	}
	return 0
}

func observerPulsePending(w *world.World, pos cube.Pos) bool {
	if w == nil {
		return false
	}
	now := w.CurrentTick()

	observerPendingMu.Lock()
	defer observerPendingMu.Unlock()

	list := observerPending[w]
	if len(list) > 0 {
		prune := 0
		for prune < len(list) && now-list[prune].tick > 4 {
			prune++
		}
		if prune > 0 {
			list = append([]observerPendingPulse(nil), list[prune:]...)
		}
	}
	for _, pending := range list {
		if pending.pos == pos {
			observerPending[w] = list
			return true
		}
	}
	list = append(list, observerPendingPulse{pos: pos, tick: now})
	observerPending[w] = list
	return false
}

func observerClearPending(w *world.World, pos cube.Pos) {
	if w == nil {
		return
	}
	now := w.CurrentTick()

	observerPendingMu.Lock()
	defer observerPendingMu.Unlock()

	list := observerPending[w]
	if len(list) == 0 {
		return
	}
	pruned := make([]observerPendingPulse, 0, len(list))
	for _, pending := range list {
		if now-pending.tick > 4 {
			continue
		}
		if pending.pos == pos {
			continue
		}
		pruned = append(pruned, pending)
	}
	if len(pruned) == 0 {
		delete(observerPending, w)
		return
	}
	observerPending[w] = pruned
}

// RedstoneStrongPower ...
func (o Observer) RedstoneStrongPower(face cube.Face) uint8 {
	return o.RedstoneWeakPower(face)
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
