package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/redstone"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// Lever is an interactable block that acts as a persistent redstone power source.
type Lever struct {
	transparent
	empty

	// Face is the face of the block the lever is attached to.
	Face cube.Face
	// Axis controls the lever's orientation when mounted on the floor or ceiling.
	Axis cube.Axis
	// Powered specifies whether the lever currently outputs redstone power.
	Powered bool
}

// UseOnBlock handles placement of the lever, ensuring it attaches to a solid face and aligns with the user.
func (l Lever) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, face, used := firstReplaceable(tx, pos, face, l)
	if !used {
		return false
	}
	supportPos := pos.Side(face.Opposite())
	if !tx.Block(supportPos).Model().FaceSolid(supportPos, face, tx) {
		return false
	}

	l.Face = face
	l.Axis = leverPlacementAxis(face, user)
	l.Powered = false

	place(tx, pos, l, user, ctx)
	queueLeverSignal(tx, pos, false)
	return placed(ctx)
}

// Activate toggles the lever and emits the corresponding redstone event.
func (l Lever) Activate(pos cube.Pos, _ cube.Face, tx *world.Tx, _ item.User, _ *item.UseContext) bool {
	l.Powered = !l.Powered
	tx.SetBlock(pos, l, nil)
	tx.PlaySound(pos.Vec3Centre(), sound.Click{})
	queueLeverSignal(tx, pos, l.Powered)
	return true
}

// NeighbourUpdateTick breaks the lever if its supporting block is removed.
func (l Lever) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	supportPos := pos.Side(l.Face.Opposite())
	if tx.Block(supportPos).Model().FaceSolid(supportPos, l.Face, tx) {
		return
	}
	queueLeverSignal(tx, pos, false)
	breakBlock(l, pos, tx)
}

// BreakInfo ...
func (l Lever) BreakInfo() BreakInfo {
	return newBreakInfo(0.5, alwaysHarvestable, nothingEffective, oneOf(Lever{})).withBreakHandler(func(pos cube.Pos, tx *world.Tx, _ item.User) {
		if l.Powered {
			queueLeverSignal(tx, pos, false)
		}
	})
}

// EncodeItem ...
func (Lever) EncodeItem() (name string, meta int16) {
	return "minecraft:lever", 0
}

// EncodeBlock ...
func (l Lever) EncodeBlock() (string, map[string]any) {
	return "minecraft:lever", map[string]any{
		"lever_direction": l.directionProperty(),
		"open_bit":        l.Powered,
	}
}

// Model ...
func (l Lever) Model() world.BlockModel {
	return model.Lever{Face: l.Face, Axis: l.Axis}
}

func (l Lever) directionProperty() string {
	switch l.Face {
	case cube.FaceUp:
		if l.Axis == cube.X {
			return "up_east_west"
		}
		return "up_north_south"
	case cube.FaceDown:
		if l.Axis == cube.X {
			return "down_east_west"
		}
		return "down_north_south"
	case cube.FaceEast:
		return "east"
	case cube.FaceWest:
		return "west"
	case cube.FaceSouth:
		return "south"
	default:
		return "north"
	}
}

func leverPlacementAxis(face cube.Face, user item.User) cube.Axis {
	switch face {
	case cube.FaceUp, cube.FaceDown:
		if user != nil {
			dir := user.Rotation().Direction()
			if dir == cube.East || dir == cube.West {
				return cube.X
			}
		}
		return cube.Z
	case cube.FaceEast, cube.FaceWest:
		return cube.Z
	default:
		return cube.X
	}
}

func queueLeverSignal(tx *world.Tx, pos cube.Pos, powered bool) {
	if powered {
		tx.QueueRedstoneEvent(pos, redstone.EventSignalRise, 15, 0)
		return
	}
	tx.QueueRedstoneEvent(pos, redstone.EventSignalFall, 0, 0)
}

// allLevers returns a list of all lever block states for registration.
func allLevers() (blocks []world.Block) {
	for _, combo := range []struct {
		face cube.Face
		axis cube.Axis
	}{
		{cube.FaceUp, cube.X},
		{cube.FaceUp, cube.Z},
		{cube.FaceDown, cube.X},
		{cube.FaceDown, cube.Z},
	} {
		blocks = append(blocks, Lever{Face: combo.face, Axis: combo.axis})
		blocks = append(blocks, Lever{Face: combo.face, Axis: combo.axis, Powered: true})
	}
	for _, face := range []cube.Face{cube.FaceNorth, cube.FaceSouth, cube.FaceEast, cube.FaceWest} {
		blocks = append(blocks, Lever{Face: face})
		blocks = append(blocks, Lever{Face: face, Powered: true})
	}
	return
}
