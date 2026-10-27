package block

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Forty strings put the opposite hook at distance 41.
const maxTripwireDistance = 41

// Tripwire is a string block that can activate tripwire hooks.
type Tripwire struct {
	empty
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

// BreakInfo ...
func (t Tripwire) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, simpleDrops(item.NewStack(item.String{}, 1)))
}

// PlayerWillDestroy updates the source while the string still exists. Native
// shears clear its powered bit; other tools set it. Both request the hook's
// delayed recheck, so the removed string later detaches the remaining line.
func (t Tripwire) PlayerWillDestroy(pos cube.Pos, tx *world.Tx, source world.DamageSource) {
	shears := false
	if damage, ok := source.(world.EntityDamageSource); ok {
		if carrier, ok := damage.SourceEntity().(item.Carrier); ok {
			main, _ := carrier.HeldItems()
			_, shears = main.Item().(item.Shears)
		}
	}
	t.Powered = !shears
	tx.SetBlock(pos, t, nil)
	updateTripwireFrom(pos, tx)
}

// Placed follows TripWireBlock::onPlace's default permutation, followed by
// connection/source updates. Same-identifier property edits retain their state.
func (t Tripwire) Placed(pos cube.Pos, tx *world.Tx, previous world.Block) {
	if _, same := previous.(Tripwire); same {
		return
	}
	initial := Tripwire{Suspended: true}
	if t != initial {
		tx.SetBlock(pos, initial, &world.SetOpts{DisableBlockUpdates: true})
	}
	updateTripwireConnections(pos, tx)
	updateTripwireFrom(pos, tx)
}

// UseOnBlock places string as BDS does, always with suspended_bit set,
// whether or not a block supports it.
func (t Tripwire) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, t)
	if !used {
		return false
	}
	t.Suspended = true
	place(tx, pos, t, user, ctx)
	updateTripwireConnections(pos, tx)
	updateTripwireFrom(pos, tx)
	return placed(ctx)
}

// NeighbourUpdateTick ...
func (t Tripwire) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	updateTripwireConnections(pos, tx)
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

func (t Tripwire) updatePowered(pos cube.Pos, tx *world.Tx) {
	powered := tripwirePowered(pos, tx)
	if powered == t.Powered {
		if powered {
			tx.ScheduleBlockUpdate(pos, t, gameTicks(10))
		}
		return
	}
	t.Powered = powered
	tx.SetBlock(pos, t, nil)
	updateTripwireFrom(pos, tx)
	if powered {
		tx.ScheduleBlockUpdate(pos, t, gameTicks(10))
	}
}

func tripwirePowered(pos cube.Pos, tx *world.Tx) bool {
	t, ok := tx.Block(pos).(Tripwire)
	if !ok {
		return false
	}
	var box cube.BBox
	if !t.Suspended {
		box = cube.Box(0, 0, 0, 1, 3.0/32, 1)
	} else if t.Attached {
		box = cube.Box(0, 1.0/16, 0, 1, 5.0/32, 1)
	} else {
		box = cube.Box(0, 0, 0, 1, 0.5, 1)
	}
	for range redstoneEntitiesInBox(tx, box.Translate(pos.Vec3()), false) {
		return true
	}
	return false
}

func updateTripwireFrom(pos cube.Pos, tx *world.Tx) {
	// The native callback searches south and west; calculateState updates
	// both end hooks and all intervening strings.
	for _, dir := range []cube.Direction{cube.South, cube.West} {
		cur := pos
		for distance := 1; distance <= maxTripwireDistance; distance++ {
			cur = cur.Side(dir.Face())
			b, _ := tx.BlockLoaded(cur)
			if _, ok := b.(Tripwire); ok {
				continue
			}
			hook, ok := b.(TripwireHook)
			if ok && hook.Facing == dir.Opposite() {
				updateTripwireLineChanged(cur, hook, tx, distance)
			}
			break
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

func updateTripwireLine(pos cube.Pos, hook TripwireHook, tx *world.Tx) {
	updateTripwireLineChanged(pos, hook, tx, -1)
}

func updateTripwireLineChanged(pos cube.Pos, hook TripwireHook, tx *world.Tx, changedIndex int) {
	facing := hook.Facing
	attached := false
	continuous := true
	powered := false
	wirePositions := make([]cube.Pos, 0, maxTripwireDistance)
	var otherPos cube.Pos
	var otherHook TripwireHook
	foundOther := false

	cur := pos
	for distance := 1; distance <= maxTripwireDistance; distance++ {
		cur = cur.Side(facing.Face())
		b, _ := tx.BlockLoaded(cur)
		if tw, ok := b.(Tripwire); ok {
			wirePositions = append(wirePositions, cur)
			if tw.Powered && !tw.Disarmed {
				powered = true
			}
			if distance == changedIndex {
				continuous = continuous && !tw.Disarmed
				tx.ScheduleBlockUpdate(pos, hook, gameTicks(10))
			}
			continue
		}
		if other, ok := b.(TripwireHook); ok {
			if other.Facing == facing.Opposite() {
				attached = distance >= 2 && continuous
				otherPos, otherHook, foundOther = cur, other, true
			}
			break
		}
		// Native calculateState continues over holes to update the opposite
		// hook and surviving strings when a later source update rescans.
		continuous = false
	}
	if !attached {
		powered = false
	}
	strength := 0
	if powered {
		strength = 15
	}
	if foundOther {
		updateHookState(otherPos, otherHook, attached, powered, tx)
		tx.Redstone().SetProducerStrength(otherPos, strength)
		// Native uses the starting hook's previous flags for both sounds.
		emitTripwireHookState(otherPos, tx, hook, attached, powered)
	}
	emitTripwireHookState(pos, tx, hook, attached, powered)
	updateHookState(pos, hook, attached, powered, tx)
	tx.Redstone().SetProducerStrength(pos, strength)
	if hook.Attached == attached || !foundOther {
		return
	}
	for _, wirePos := range wirePositions {
		updateTripwireConnections(wirePos, tx)
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
