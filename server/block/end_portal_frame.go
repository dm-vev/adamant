package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// EndPortalFrame is a decorative block that completes end portals when activated with Eyes of Ender.
type EndPortalFrame struct {
	solid

	// Facing is the direction the frame is facing.
	Facing cube.Direction
	// Eye specifies if an Eye of Ender has been placed into the frame.
	Eye bool
}

// UseOnBlock handles the placing of end portal frames and orients them based on the user facing direction.
func (f EndPortalFrame) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, f)
	if !used {
		return false
	}

	f.Facing = user.Rotation().Direction()
	place(tx, pos, f, user, ctx)
	return placed(ctx)
}

// Activate handles inserting eyes of ender into the frame.
func (f EndPortalFrame) Activate(pos cube.Pos, _ cube.Face, tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	held, _ := u.HeldItems()
	if held.Empty() {
		return false
	}
	if _, ok := held.Item().(item.EyeOfEnder); !ok {
		return false
	}

	if f.Eye {
		return true
	}

	f.Eye = true
	tx.SetBlock(pos, f, nil)
	tx.PlaySound(pos.Vec3Centre(), sound.BlockPlace{Block: f})
	ctx.SubtractFromCount(1)
	tryCreateEndPortal(tx, pos)
	return true
}

// BreakInfo returns information on how the frame should be broken.
func (f EndPortalFrame) BreakInfo() BreakInfo {
	return newBreakInfo(22.5, pickaxeHarvestable, pickaxeEffective, oneOf(f)).withBlastResistance(3600)
}

// EncodeItem returns the ID and metadata for the end portal frame item.
func (EndPortalFrame) EncodeItem() (name string, meta int16) {
	return "minecraft:end_portal_frame", 0
}

// EncodeBlock returns the block state for the frame.
func (f EndPortalFrame) EncodeBlock() (string, map[string]any) {
	return "minecraft:end_portal_frame", map[string]any{
		"minecraft:cardinal_direction": f.Facing.String(),
		"end_portal_eye_bit":           f.Eye,
	}
}

// allEndPortalFrames returns all block states for end portal frames.
func allEndPortalFrames() (frames []world.Block) {
	for _, d := range cube.Directions() {
		frames = append(frames, EndPortalFrame{Facing: d})
		frames = append(frames, EndPortalFrame{Facing: d, Eye: true})
	}
	return
}

func tryCreateEndPortal(tx *world.Tx, placed cube.Pos) {
	for _, orientation := range endPortalOrientations {
		axisX := directionVec(orientation.x)
		axisZ := directionVec(orientation.z)
		for _, offset := range endPortalFrameOffsets {
			origin := placed.Sub(applyEndPortalOffset(offset, axisX, axisZ))
			if endPortalRingComplete(tx, origin, orientation.x, orientation.z, axisX, axisZ) {
				fillEndPortal(tx, origin, axisX, axisZ)
				return
			}
		}
	}
}

func endPortalRingComplete(tx *world.Tx, origin cube.Pos, dirX, dirZ cube.Direction, axisX, axisZ cube.Pos) bool {
	rng := tx.Range()
	for _, offset := range endPortalFrameOffsets {
		pos := origin.Add(applyEndPortalOffset(offset, axisX, axisZ))
		if pos.OutOfBounds(rng) {
			return false
		}
		frame, ok := tx.Block(pos).(EndPortalFrame)
		if !ok || !frame.Eye || frame.Facing != facingForOffset(offset, dirX, dirZ) {
			return false
		}
	}
	for _, offset := range endPortalInteriorOffsets {
		pos := origin.Add(applyEndPortalOffset(offset, axisX, axisZ))
		if pos.OutOfBounds(rng) {
			return false
		}
		switch tx.Block(pos).(type) {
		case EndPortal, Air:
			continue
		default:
			return false
		}
	}
	return true
}

func fillEndPortal(tx *world.Tx, origin cube.Pos, axisX, axisZ cube.Pos) {
	for _, offset := range endPortalInteriorOffsets {
		pos := origin.Add(applyEndPortalOffset(offset, axisX, axisZ))
		tx.SetBlock(pos, EndPortal{}, nil)
	}
}

func directionVec(d cube.Direction) cube.Pos {
	switch d {
	case cube.North:
		return cube.Pos{0, 0, -1}
	case cube.South:
		return cube.Pos{0, 0, 1}
	case cube.West:
		return cube.Pos{-1, 0, 0}
	case cube.East:
		return cube.Pos{1, 0, 0}
	}
	panic("invalid direction")
}

func applyEndPortalOffset(offset endPortalOffset, axisX, axisZ cube.Pos) cube.Pos {
	pos := cube.Pos{}
	if offset.X != 0 {
		pos = pos.Add(mulPos(axisX, offset.X))
	}
	if offset.Z != 0 {
		pos = pos.Add(mulPos(axisZ, offset.Z))
	}
	return pos
}

func mulPos(p cube.Pos, f int) cube.Pos {
	return cube.Pos{p[0] * f, p[1] * f, p[2] * f}
}

func facingForOffset(offset endPortalOffset, dirX, dirZ cube.Direction) cube.Direction {
	switch {
	case offset.X == -1:
		return dirX
	case offset.X == 3:
		return dirX.Opposite()
	case offset.Z == -1:
		return dirZ
	default:
		return dirZ.Opposite()
	}
}

type endPortalOffset struct {
	X int
	Z int
}

var (
	endPortalFrameOffsets = []endPortalOffset{
		{-1, 0}, {-1, 1}, {-1, 2},
		{3, 0}, {3, 1}, {3, 2},
		{0, -1}, {1, -1}, {2, -1},
		{0, 3}, {1, 3}, {2, 3},
	}
	endPortalInteriorOffsets = []endPortalOffset{
		{0, 0}, {1, 0}, {2, 0},
		{0, 1}, {1, 1}, {2, 1},
		{0, 2}, {1, 2}, {2, 2},
	}
	endPortalOrientations = []struct {
		x cube.Direction
		z cube.Direction
	}{
		{cube.East, cube.South},
		{cube.South, cube.West},
		{cube.West, cube.North},
		{cube.North, cube.East},
	}
)
