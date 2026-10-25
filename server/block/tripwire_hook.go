package block

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// TripwireHook is a redstone component that detects a tripwire line.
type TripwireHook struct {
	empty
	transparent

	Facing   cube.Direction
	Attached bool
	Powered  bool
}

// BreakInfo ...
func (t TripwireHook) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, oneOf(TripwireHook{})).withBreakHandler(func(pos cube.Pos, tx *world.Tx, _ item.User) {
		t.removed(pos, tx)
	})
}

// removed ports TripWireHookBlock.onRemoved: a hook taken away detaches its
// line and lets go of the power it gave.
func (t TripwireHook) removed(pos cube.Pos, tx *world.Tx) {
	if t.Attached || t.Powered {
		calculateTripwireHook(pos, t, true, false, -1, nil, tx)
	}
	if t.Powered {
		notifyTripwireHookNeighbours(pos, t.Facing, tx)
	}
}

// UseOnBlock ...
func (t TripwireHook) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, face, used := firstReplaceable(tx, pos, face, t)
	if !used {
		return false
	}
	if face.Axis() == cube.Y {
		return false
	}
	if !tx.Block(pos.Side(face.Opposite())).Model().FaceSolid(pos.Side(face.Opposite()), face, tx) {
		return false
	}
	t.Facing = face.Direction()
	t.Attached, t.Powered = false, false
	place(tx, pos, t, user, ctx)
	if placed(ctx) {
		// TripWireHookBlock.setPlacedBy.
		calculateTripwireHook(pos, t, false, false, -1, nil, tx)
	}
	return placed(ctx)
}

// NeighbourUpdateTick ports TripWireHookBlock.updateShape: a hook falls off
// once the face it hangs on goes.
func (t TripwireHook) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	attachedPos := pos.Side(t.Facing.Opposite().Face())
	if !tx.Block(attachedPos).Model().FaceSolid(attachedPos, t.Facing.Face(), tx) {
		breakBlock(t, pos, tx)
	}
}

// ScheduledTick ports TripWireHookBlock.tick.
func (t TripwireHook) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	calculateTripwireHook(pos, t, false, true, -1, nil, tx)
}

// calculateTripwireHook ports TripWireHookBlock.calculateState: the hook at
// pos looks along its line of string for a hook facing it. Both are attached
// if the line between them is whole and they are more than a block apart,
// and powered if armed string of the line is pressed. The string at the
// distance wireSource, if not -1, is taken to be in the state passed.
func calculateTripwireHook(pos cube.Pos, hook TripwireHook, destroyed, canUpdate bool, wireSource int, wireSourceState *Tripwire, tx *world.Tx) {
	dir := hook.Facing
	wasAttached, wasPowered := hook.Attached, hook.Powered
	attached, powered := !destroyed, false
	receiver := 0
	var wires [tripwireMaxDistance]*Tripwire
	for i := 1; i < tripwireMaxDistance; i++ {
		test := tripwireStep(pos, dir, i)
		b := tx.Block(test)
		if other, ok := b.(TripwireHook); ok {
			if other.Facing == dir.Opposite() {
				receiver = i
			}
			break
		}
		wire, isWire := b.(Tripwire)
		if !isWire && i != wireSource {
			attached = false
			continue
		}
		if i == wireSource && wireSourceState != nil {
			wire = *wireSourceState
		}
		armed := !wire.Disarmed
		powered = powered || armed && wire.Powered
		wires[i] = &wire
		if i == wireSource {
			tx.ScheduleBlockUpdate(pos, hook, gameTicks(10))
			attached = attached && armed
		}
	}
	attached = attached && receiver > 1
	powered = powered && attached
	if receiver > 0 {
		test := tripwireStep(pos, dir, receiver)
		tx.SetBlock(test, TripwireHook{Facing: dir.Opposite(), Attached: attached, Powered: powered}, nil)
		notifyTripwireHookNeighbours(test, dir.Opposite(), tx)
		if _, ok := tx.Block(pos).(TripwireHook); !ok {
			TripwireHook{Facing: dir, Attached: attached, Powered: powered}.removed(pos, tx)
			return
		}
		emitTripwireHookState(test, tx, attached, powered, wasAttached, wasPowered)
	}
	emitTripwireHookState(pos, tx, attached, powered, wasAttached, wasPowered)
	if !destroyed {
		tx.SetBlock(pos, TripwireHook{Facing: dir, Attached: attached, Powered: powered}, nil)
		if canUpdate {
			notifyTripwireHookNeighbours(pos, dir, tx)
		}
	}
	if wasAttached != attached {
		for i := 1; i < receiver; i++ {
			if wires[i] == nil {
				continue
			}
			test := tripwireStep(pos, dir, i)
			if _, ok := tx.Block(test).(Tripwire); ok {
				wire := *wires[i]
				wire.Attached = attached
				tx.SetBlock(test, wire, nil)
			}
		}
	}
}

// emitTripwireHookState ports TripWireHookBlock.emitState: the click of a
// hook powering on or off, or else the sound of its line attaching or
// detaching.
func emitTripwireHookState(pos cube.Pos, tx *world.Tx, attached, powered, wasAttached, wasPowered bool) {
	var event string
	switch {
	case powered && !wasPowered:
		event = "power.on"
	case !powered && wasPowered:
		event = "power.off"
	case attached && !wasAttached:
		event = "attach"
	case !attached && wasAttached:
		event = "detach"
	default:
		return
	}
	tx.PlaySound(pos.Vec3Centre(), sound.NativeEvent{SoundType: event, ExtraData: int32(world.BlockRuntimeID(tx.Block(pos)))})
}

// notifyTripwireHookNeighbours ports TripWireHookBlock.notifyNeighbors: the
// blocks around a hook and around the block it hangs on are updated.
func notifyTripwireHookNeighbours(pos cube.Pos, facing cube.Direction, tx *world.Tx) {
	tx.DoBlockUpdatesAround(pos)
	tx.DoBlockUpdatesAround(pos.Side(facing.Opposite().Face()))
}

func (t TripwireHook) RedstonePower(cube.Pos, *world.Tx, cube.Face) int {
	if t.Powered {
		return 15
	}
	return 0
}

func (t TripwireHook) RedstoneStrongPower(_ cube.Pos, _ *world.Tx, face cube.Face) int {
	if !t.Powered {
		return 0
	}
	if face == t.Facing.Opposite().Face() {
		return 15
	}
	return 0
}

// EncodeItem ...
func (TripwireHook) EncodeItem() (name string, meta int16) {
	return "minecraft:tripwire_hook", 0
}

// EncodeBlock ...
func (t TripwireHook) EncodeBlock() (string, map[string]any) {
	return "minecraft:tripwire_hook", map[string]any{
		"attached_bit": boolByte(t.Attached),
		"direction":    int32(horizontalDirection(t.Facing)),
		"powered_bit":  boolByte(t.Powered),
	}
}

func allTripwireHooks() (hooks []world.Block) {
	for _, facing := range cube.Directions() {
		for _, attached := range []bool{false, true} {
			for _, powered := range []bool{false, true} {
				hooks = append(hooks, TripwireHook{Facing: facing, Attached: attached, Powered: powered})
			}
		}
	}
	return
}

// tripwireStep returns the position n blocks from pos in the direction dir.
func tripwireStep(pos cube.Pos, dir cube.Direction, n int) cube.Pos {
	off := cube.Pos{}.Side(dir.Face())
	return cube.Pos{pos[0] + off[0]*n, pos[1], pos[2] + off[2]*n}
}
