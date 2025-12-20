package block

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

const maxTripwireLength = 40

// Tripwire is a string block that can activate tripwire hooks.
type Tripwire struct {
	carpet
	transparent
	replaceable
	sourceWaterDisplacer

	Attached  bool
	Disarmed  bool
	Powered   bool
	Suspended bool
}

// BreakInfo ...
func (t Tripwire) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, simpleDrops(item.NewStack(item.String{}, 1)))
}

// UseOnBlock ...
func (t Tripwire) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, t)
	if !used {
		return false
	}
	t.Suspended = !tx.Block(pos.Side(cube.FaceDown)).Model().FaceSolid(pos.Side(cube.FaceDown), cube.FaceUp, tx)
	place(tx, pos, t, user, ctx)
	updateTripwireFrom(pos, tx)
	return placed(ctx)
}

// NeighbourUpdateTick ...
func (t Tripwire) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	suspended := !tx.Block(pos.Side(cube.FaceDown)).Model().FaceSolid(pos.Side(cube.FaceDown), cube.FaceUp, tx)
	if suspended != t.Suspended {
		t.Suspended = suspended
		tx.SetBlock(pos, t, nil)
	}
	updateTripwireFrom(pos, tx)
}

// EntityInside ...
func (t Tripwire) EntityInside(pos cube.Pos, tx *world.Tx, _ world.Entity) {
	if t.Powered {
		return
	}
	t.updatePowered(pos, tx)
}

// ScheduledTick ...
func (t Tripwire) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	t.updatePowered(pos, tx)
}

// EncodeItem ...
func (Tripwire) EncodeItem() (name string, meta int16) {
	return "minecraft:string", 0
}

// EncodeBlock ...
func (t Tripwire) EncodeBlock() (string, map[string]any) {
	return "minecraft:trip_wire", map[string]any{
		"attached_bit":  boolByte(t.Attached),
		"disarmed_bit":  boolByte(t.Disarmed),
		"powered_bit":   boolByte(t.Powered),
		"suspended_bit": boolByte(t.Suspended),
	}
}

func (t Tripwire) updatePowered(pos cube.Pos, tx *world.Tx) {
	powered := tripwirePowered(pos, tx)
	if powered == t.Powered {
		if powered {
			tx.ScheduleBlockUpdate(pos, t, redstoneTicks(1))
		}
		return
	}
	t.Powered = powered
	tx.SetBlock(pos, t, nil)
	updateTripwireFrom(pos, tx)
	if powered {
		tx.ScheduleBlockUpdate(pos, t, redstoneTicks(1))
	}
}

func tripwirePowered(pos cube.Pos, tx *world.Tx) bool {
	box := cube.Box(float64(pos[0]), float64(pos[1]), float64(pos[2]), float64(pos[0]+1), float64(pos[1]+1), float64(pos[2]+1))
	for range tx.EntitiesWithin(box) {
		return true
	}
	return false
}

func updateTripwireFrom(pos cube.Pos, tx *world.Tx) {
	for _, dir := range cube.Directions() {
		cur := pos
		for i := 0; i < maxTripwireLength; i++ {
			cur = cur.Side(dir.Face())
			b := tx.Block(cur)
			if _, ok := b.(Tripwire); ok {
				continue
			}
			hook, ok := b.(TripwireHook)
			if ok && hook.Facing == dir {
				updateTripwireLine(cur, hook, tx)
			}
			break
		}
	}
}

func updateTripwireLine(pos cube.Pos, hook TripwireHook, tx *world.Tx) {
	facing := hook.Facing
	attached := false
	powered := false
	wirePositions := make([]cube.Pos, 0, maxTripwireLength)

	cur := pos
	for i := 0; i < maxTripwireLength; i++ {
		cur = cur.Side(facing.Face())
		b := tx.Block(cur)
		if tw, ok := b.(Tripwire); ok {
			wirePositions = append(wirePositions, cur)
			if tw.Powered {
				powered = true
			}
			continue
		}
		if other, ok := b.(TripwireHook); ok && other.Facing == facing.Opposite() {
			attached = true
			updateHookState(cur, other, attached, powered, tx)
		}
		break
	}
	if !attached {
		powered = false
	}
	updateHookState(pos, hook, attached, powered, tx)
	for _, wirePos := range wirePositions {
		tw := tx.Block(wirePos).(Tripwire)
		if tw.Attached != attached {
			tw.Attached = attached
			tx.SetBlock(wirePos, tw, nil)
		}
	}
}

func updateHookState(pos cube.Pos, hook TripwireHook, attached, powered bool, tx *world.Tx) {
	if hook.Attached == attached && hook.Powered == powered {
		return
	}
	hook.Attached = attached
	hook.Powered = powered
	tx.SetBlock(pos, hook, nil)
	tx.DoBlockUpdatesAround(pos.Side(hook.Facing.Opposite().Face()))
}

func allTripwires() (wires []world.Block) {
	for _, attached := range []bool{false, true} {
		for _, disarmed := range []bool{false, true} {
			for _, powered := range []bool{false, true} {
				for _, suspended := range []bool{false, true} {
					wires = append(wires, Tripwire{Attached: attached, Disarmed: disarmed, Powered: powered, Suspended: suspended})
				}
			}
		}
	}
	return
}
