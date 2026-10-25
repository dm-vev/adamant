package block

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Tripwire is a string block that can activate tripwire hooks.
type Tripwire struct {
	carpet
	transparent
	replaceable
	sourceWaterDisplacer

	Attached        bool
	Disarmed        bool
	Powered         bool
	Suspended       bool
	NorthConnection bool
	EastConnection  bool
	SouthConnection bool
	WestConnection  bool
}

// BreakInfo ports TripWireBlock.affectNeighborsAfterRemoval: string taken
// away trips the hooks of its line, as if pressed, unless shears cut it,
// disarming it as TripWireBlock.playerWillDestroy does.
func (t Tripwire) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, simpleDrops(item.NewStack(item.String{}, 1))).withBreakHandler(func(pos cube.Pos, tx *world.Tx, u item.User) {
		if u != nil {
			if held, _ := u.HeldItems(); held.Item() != nil {
				if _, shears := held.Item().(item.Shears); shears {
					t.Disarmed = true
				}
			}
		}
		t.Powered = true
		updateTripwireSource(pos, t, tx)
	})
}

// UseOnBlock places string as BDS does, always with suspended_bit set,
// whether or not a block supports it.
func (t Tripwire) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, t)
	if !used {
		return false
	}
	t.Suspended = true
	t.NorthConnection = tripwireConnectsFrom(tx, pos, cube.North)
	t.EastConnection = tripwireConnectsFrom(tx, pos, cube.East)
	t.SouthConnection = tripwireConnectsFrom(tx, pos, cube.South)
	t.WestConnection = tripwireConnectsFrom(tx, pos, cube.West)
	place(tx, pos, t, user, ctx)
	if placed(ctx) {
		// TripWireBlock.onPlace.
		updateTripwireSource(pos, t, tx)
	}
	return placed(ctx)
}

// NeighbourUpdateTick ports TripWireBlock.updateShape: string connects to
// the string and facing hooks beside it.
func (t Tripwire) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	updateTripwireConnections(pos, tx)
}

// tripwireShape is the shape of string: low over the ground when attached
// to hooks, half a block high otherwise.
func (t Tripwire) tripwireShape() cube.BBox {
	if t.Attached {
		return cube.Box(0, 1.0/16, 0, 1, 2.5/16, 1)
	}
	return cube.Box(0, 0, 0, 1, 0.5, 1)
}

// EntityInside ports TripWireBlock.entityInside: an entity in the shape of
// unpowered string presses it.
func (t Tripwire) EntityInside(pos cube.Pos, tx *world.Tx, e world.Entity) {
	if t.Powered || tx.HasScheduledBlockUpdate(pos, t) {
		return
	}
	if !e.H().Type().BBox(e).Translate(e.Position()).IntersectsWith(t.tripwireShape().Translate(pos.Vec3())) {
		return
	}
	t.checkPressed(pos, tx, true)
}

// ScheduledTick ports TripWireBlock.tick: pressed string checks whether it
// still is.
func (t Tripwire) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	if t.Powered {
		t.checkPressed(pos, tx, tripwirePressed(pos, t, tx))
	}
}

// checkPressed ports TripWireBlock.checkPressed: string pressed or let go
// updates the hooks of its line, and checks again in 10 ticks while pressed,
// or the next tick once let go.
func (t Tripwire) checkPressed(pos cube.Pos, tx *world.Tx, pressed bool) {
	was := t.Powered
	if pressed != was {
		t.Powered = pressed
		tx.SetBlock(pos, t, nil)
		updateTripwireSource(pos, t, tx)
	}
	if pressed {
		tx.ScheduleBlockUpdate(pos, t, gameTicks(10))
	} else if was {
		tx.ScheduleBlockUpdate(pos, t, gameTicks(1))
	}
}

// EncodeItem ...
func (Tripwire) EncodeItem() (name string, meta int16) {
	return "minecraft:string", 0
}

// EncodeBlock ...
func (t Tripwire) EncodeBlock() (string, map[string]any) {
	return "minecraft:trip_wire", map[string]any{
		"attached_bit":               boolByte(t.Attached),
		"disarmed_bit":               boolByte(t.Disarmed),
		"powered_bit":                boolByte(t.Powered),
		"suspended_bit":              boolByte(t.Suspended),
		"minecraft:connection_north": boolByte(t.NorthConnection),
		"minecraft:connection_east":  boolByte(t.EastConnection),
		"minecraft:connection_south": boolByte(t.SouthConnection),
		"minecraft:connection_west":  boolByte(t.WestConnection),
	}
}

// tripwirePressed reports if an entity is within the shape of the string.
func tripwirePressed(pos cube.Pos, t Tripwire, tx *world.Tx) bool {
	for range tx.EntitiesWithin(t.tripwireShape().Translate(pos.Vec3())) {
		return true
	}
	return false
}

// tripwireMaxDistance is how far apart, exclusive, two hooks of one line
// may be.
const tripwireMaxDistance = 42

// updateTripwireSource ports TripWireBlock.updateSource: the hook at the
// south or west end of the line of string through pos recalculates the
// line, taking the string at pos to be in the state passed.
func updateTripwireSource(pos cube.Pos, state Tripwire, tx *world.Tx) {
	for _, dir := range []cube.Direction{cube.South, cube.West} {
		cur := pos
		for i := 1; i < tripwireMaxDistance; i++ {
			cur = cur.Side(dir.Face())
			b := tx.Block(cur)
			if hook, ok := b.(TripwireHook); ok {
				if hook.Facing == dir.Opposite() {
					calculateTripwireHook(cur, hook, false, true, i, &state, tx)
				}
				break
			}
			if _, ok := b.(Tripwire); !ok {
				break
			}
		}
	}
}

func updateTripwireConnections(pos cube.Pos, tx *world.Tx) {
	t, ok := tx.Block(pos).(Tripwire)
	if !ok {
		return
	}
	connections := [4]bool{}
	for _, dir := range cube.Directions() {
		neighbourPos := pos.Side(dir.Face())
		neighbour := tx.Block(neighbourPos)
		switch neighbour := neighbour.(type) {
		case Tripwire:
			connections[dir] = true
		case TripwireHook:
			connections[dir] = neighbour.Facing == dir.Opposite()
		}
	}
	if t.NorthConnection == connections[cube.North] && t.EastConnection == connections[cube.East] &&
		t.SouthConnection == connections[cube.South] && t.WestConnection == connections[cube.West] {
		return
	}
	t.NorthConnection = connections[cube.North]
	t.EastConnection = connections[cube.East]
	t.SouthConnection = connections[cube.South]
	t.WestConnection = connections[cube.West]
	tx.SetBlock(pos, t, &world.SetOpts{DisableBlockUpdates: true})
}

func (t Tripwire) ReconstructMissingState(pos cube.Pos, missing []string, bs world.BlockSource) world.Block {
	if hasMissingProperty(missing, "minecraft:connection_north") {
		t.NorthConnection = tripwireConnectsFrom(bs, pos, cube.North)
	}
	if hasMissingProperty(missing, "minecraft:connection_east") {
		t.EastConnection = tripwireConnectsFrom(bs, pos, cube.East)
	}
	if hasMissingProperty(missing, "minecraft:connection_south") {
		t.SouthConnection = tripwireConnectsFrom(bs, pos, cube.South)
	}
	if hasMissingProperty(missing, "minecraft:connection_west") {
		t.WestConnection = tripwireConnectsFrom(bs, pos, cube.West)
	}
	return t
}

func tripwireConnectsFrom(bs world.BlockSource, pos cube.Pos, dir cube.Direction) bool {
	neighbour := bs.Block(pos.Side(dir.Face()))
	switch neighbour := neighbour.(type) {
	case Tripwire:
		return true
	case TripwireHook:
		return neighbour.Facing == dir.Opposite()
	default:
		return false
	}
}
func allTripwires() []world.Block {
	wires := make([]world.Block, 256)
	for mask := range 256 {
		wires[mask] = Tripwire{
			Attached: mask&128 != 0, Disarmed: mask&64 != 0,
			Powered: mask&32 != 0, Suspended: mask&16 != 0,
			NorthConnection: mask&8 != 0, EastConnection: mask&4 != 0,
			SouthConnection: mask&2 != 0, WestConnection: mask&1 != 0,
		}
	}
	return wires
}
