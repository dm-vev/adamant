package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Lever is a redstone component that toggles power.
type Lever struct {
	transparent
	empty

	Orientation LeverOrientation
	Powered     bool
}

// BreakInfo ...
func (l Lever) BreakInfo() BreakInfo {
	return newBreakInfo(0.5, alwaysHarvestable, nothingEffective, oneOf(l))
}

// UseOnBlock ...
func (l Lever) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, face, used := firstReplaceable(tx, pos, face, l)
	if !used {
		return false
	}
	orientation := leverOrientationFor(face, user.Rotation().Direction())
	if !tx.Block(pos.Side(orientation.SupportDirection())).Model().FaceSolid(pos.Side(orientation.SupportDirection()), orientation.SupportFace(), tx) {
		return false
	}
	l.Orientation = orientation
	place(tx, pos, l, user, ctx)
	return placed(ctx)
}

// Activate ...
func (l Lever) Activate(pos cube.Pos, _ cube.Face, tx *world.Tx, _ item.User, _ *item.UseContext) bool {
	l.Powered = !l.Powered
	tx.SetBlock(pos, l, nil)
	tx.DoBlockUpdatesAround(pos.Side(l.Orientation.SupportDirection()))
	return true
}

// NeighbourUpdateTick ...
func (l Lever) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !tx.Block(pos.Side(l.Orientation.SupportDirection())).Model().FaceSolid(pos.Side(l.Orientation.SupportDirection()), l.Orientation.SupportFace(), tx) {
		breakBlock(l, pos, tx)
	}
}

// RedstoneWeakPower ...
func (l Lever) RedstoneWeakPower(cube.Face) uint8 {
	if l.Powered {
		return 15
	}
	return 0
}

// RedstoneStrongPower ...
func (l Lever) RedstoneStrongPower(face cube.Face) uint8 {
	if !l.Powered {
		return 0
	}
	if face == l.Orientation.SupportDirection() {
		return 15
	}
	return 0
}

// EncodeItem ...
func (l Lever) EncodeItem() (name string, meta int16) {
	return "minecraft:lever", 0
}

// EncodeBlock ...
func (l Lever) EncodeBlock() (string, map[string]any) {
	return "minecraft:lever", map[string]any{"lever_direction": l.Orientation.String(), "open_bit": boolByte(l.Powered)}
}

// LeverOrientation represents the attachment direction of a lever.
type LeverOrientation uint8

const (
	leverDownEastWest LeverOrientation = iota
	leverEast
	leverWest
	leverSouth
	leverNorth
	leverUpNorthSouth
	leverUpEastWest
	leverDownNorthSouth
)

func leverOrientationFor(face cube.Face, playerFacing cube.Direction) LeverOrientation {
	switch face {
	case cube.FaceDown:
		if isXAxis(playerFacing) {
			return leverDownEastWest
		}
		return leverDownNorthSouth
	case cube.FaceUp:
		if isXAxis(playerFacing) {
			return leverUpEastWest
		}
		return leverUpNorthSouth
	case cube.FaceNorth:
		return leverNorth
	case cube.FaceSouth:
		return leverSouth
	case cube.FaceWest:
		return leverWest
	case cube.FaceEast:
		return leverEast
	}
	return leverDownEastWest
}

func (l LeverOrientation) String() string {
	switch l {
	case leverDownEastWest:
		return "down_east_west"
	case leverEast:
		return "east"
	case leverWest:
		return "west"
	case leverSouth:
		return "south"
	case leverNorth:
		return "north"
	case leverUpNorthSouth:
		return "up_north_south"
	case leverUpEastWest:
		return "up_east_west"
	case leverDownNorthSouth:
		return "down_north_south"
	}
	return "down_east_west"
}

func (l LeverOrientation) Uint8() uint8 {
	return uint8(l)
}

func (l LeverOrientation) SupportFace() cube.Face {
	switch l {
	case leverDownEastWest, leverDownNorthSouth:
		return cube.FaceDown
	case leverUpEastWest, leverUpNorthSouth:
		return cube.FaceUp
	case leverNorth:
		return cube.FaceNorth
	case leverSouth:
		return cube.FaceSouth
	case leverWest:
		return cube.FaceWest
	case leverEast:
		return cube.FaceEast
	}
	return cube.FaceDown
}

func (l LeverOrientation) SupportDirection() cube.Face {
	return l.SupportFace().Opposite()
}

func isXAxis(d cube.Direction) bool {
	return d == cube.East || d == cube.West
}

func allLevers() (levers []world.Block) {
	for _, powered := range []bool{false, true} {
		for o := leverDownEastWest; o <= leverDownNorthSouth; o++ {
			levers = append(levers, Lever{Orientation: o, Powered: powered})
		}
	}
	return
}
