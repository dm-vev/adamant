package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

const bellRingTicks = 50

// Bell is a transparent block that can be placed on the floor, ceiling, or walls and rung by interacting with it.
// Village and raid behaviour is intentionally not implemented.
type Bell struct {
	transparent
	sourceWaterDisplacer

	// Attach represents the attachment type of the Bell.
	Attach BellAttachment
	// Facing represents the horizontal direction of the Bell.
	Facing cube.Direction
	// Toggle is true while the Bell is ringing.
	Toggle bool

	ringTicks     int
	ringDirection cube.Face
}

// BreakInfo ...
func (b Bell) BreakInfo() BreakInfo {
	return newBreakInfo(5, alwaysHarvestable, pickaxeEffective, oneOf(b)).withBlastResistance(5)
}

// Activate ...
func (b Bell) Activate(pos cube.Pos, face cube.Face, tx *world.Tx, _ item.User, _ *item.UseContext) bool {
	return b.Ring(pos, tx, face)
}

// ProjectileHit ...
func (b Bell) ProjectileHit(pos cube.Pos, tx *world.Tx, _ world.Entity, face cube.Face) {
	b.Ring(pos, tx, face)
}

// Ring starts or refreshes the Bell ring state in the direction of face.
func (b Bell) Ring(pos cube.Pos, tx *world.Tx, face cube.Face) bool {
	if !b.canRing(face) {
		return false
	}
	b.Toggle, b.ringTicks = true, 0
	b.ringDirection = face
	tx.SetBlock(pos, b, &world.SetOpts{DisableBlockUpdates: true, DisableLiquidDisplacement: true})
	tx.PlaySound(pos.Vec3Centre(), sound.BellRing{})
	return true
}

// UseOnBlock ...
func (b Bell) UseOnBlock(
	pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext,
) bool {
	pos, face, used := firstReplaceable(tx, pos, face, b)
	if !used {
		return false
	}
	switch face {
	case cube.FaceUp:
		if !bellSupportSolid(tx, pos, cube.FaceDown) {
			return false
		}
		b.Attach = StandingBellAttachment()
		b.Facing = user.Rotation().Direction().Opposite()
	case cube.FaceDown:
		if !bellSupportSolid(tx, pos, cube.FaceUp) {
			return false
		}
		b.Attach = HangingBellAttachment()
		b.Facing = user.Rotation().Direction().Opposite()
	default:
		if !bellSupportSolid(tx, pos, face.Opposite()) {
			return false
		}
		b.Attach = SideBellAttachment()
		b.Facing = face.Direction()
		if bellSupportSolid(tx, pos, face) {
			b.Attach = MultipleBellAttachment()
		}
	}
	place(tx, pos, b, user, ctx)
	return placed(ctx)
}

// NeighbourUpdateTick ...
func (b Bell) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	switch b.Attach {
	case StandingBellAttachment():
		if !bellSupportSolid(tx, pos, cube.FaceDown) {
			breakBlock(b, pos, tx)
		}
	case HangingBellAttachment():
		if !bellSupportSolid(tx, pos, cube.FaceUp) {
			breakBlock(b, pos, tx)
		}
	case SideBellAttachment():
		if !bellSupportSolid(tx, pos, b.Facing.Face().Opposite()) {
			breakBlock(b, pos, tx)
		}
	case MultipleBellAttachment():
		face := b.Facing.Face()
		positiveSolid := bellSupportSolid(tx, pos, face)
		negativeSolid := bellSupportSolid(tx, pos, face.Opposite())
		switch {
		case positiveSolid && negativeSolid:
		case positiveSolid:
			b.Attach = SideBellAttachment()
			b.Facing = face.Opposite().Direction()
			tx.SetBlock(pos, b, &world.SetOpts{DisableBlockUpdates: true, DisableLiquidDisplacement: true})
		case negativeSolid:
			b.Attach = SideBellAttachment()
			tx.SetBlock(pos, b, &world.SetOpts{DisableBlockUpdates: true, DisableLiquidDisplacement: true})
		default:
			breakBlock(b, pos, tx)
		}
	}
}

// Tick ...
func (b Bell) Tick(_ int64, pos cube.Pos, tx *world.Tx) {
	if !b.Toggle {
		return
	}
	b.ringTicks++
	if b.ringTicks >= bellRingTicks {
		b.Toggle, b.ringTicks = false, 0
	}
	tx.SetBlock(pos, b, &world.SetOpts{DisableBlockUpdates: true, DisableLiquidDisplacement: true})
}

// Model ...
func (b Bell) Model() world.BlockModel {
	return model.Bell{Attachment: model.BellAttachment(b.Attach.Uint8()), Facing: b.Facing}
}

// EncodeBlock ...
func (b Bell) EncodeBlock() (string, map[string]any) {
	return "minecraft:bell", map[string]any{
		"attachment": b.Attach.String(),
		"direction":  int32(horizontalDirection(b.Facing)),
		"toggle_bit": boolByte(b.Toggle),
	}
}

// EncodeItem ...
func (Bell) EncodeItem() (name string, meta int16) {
	return "minecraft:bell", 0
}

// EncodeNBT ...
func (b Bell) EncodeNBT() map[string]any {
	return map[string]any{
		"id":        "Bell",
		"Ringing":   boolByte(b.Toggle),
		"Ticks":     int32(b.ringTicks),
		"Direction": int32(b.ringFace()),
	}
}

// DecodeNBT ...
func (b Bell) DecodeNBT(data map[string]any) any {
	b.Toggle = nbtconv.Bool(data, "Ringing")
	b.ringTicks = int(nbtconv.Int32(data, "Ticks"))
	b.ringDirection = b.validRingFace(cube.Face(nbtconv.Int32(data, "Direction")))
	return b
}

// SideClosed ...
func (Bell) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool {
	return false
}

func (b Bell) canRing(face cube.Face) bool {
	return face.Axis() != cube.Y && face.Axis() != b.Facing.Face().Axis()
}

func (b Bell) ringFace() cube.Face {
	return b.validRingFace(b.ringDirection)
}

func (b Bell) validRingFace(face cube.Face) cube.Face {
	if b.canRing(face) {
		return face
	}
	return b.Facing.Face().RotateRight()
}

func bellSupportSolid(tx *world.Tx, pos cube.Pos, face cube.Face) bool {
	supportPos := pos.Side(face)
	return tx.Block(supportPos).Model().FaceSolid(supportPos, face.Opposite(), tx)
}

// allBells ...
func allBells() (bells []world.Block) {
	for _, a := range BellAttachments() {
		for _, d := range cube.Directions() {
			bells = append(bells, Bell{Attach: a, Facing: d})
			bells = append(bells, Bell{Attach: a, Facing: d, Toggle: true})
		}
	}
	return
}
