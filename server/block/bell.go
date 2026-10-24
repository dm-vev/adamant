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

// bellRingTicks is the Ticks count at which a Bell stops ringing and its
// idle count wraps. BDS counts two a tick while ringing and one while still.
const bellRingTicks = 50

// Bell is a transparent block that can be placed on the floor, ceiling, or walls and rung by interacting with it.
// Java's raider glowing has no Bedrock representation.
type Bell struct {
	transparent
	sourceWaterDisplacer

	// Attach represents the attachment type of the Bell.
	Attach BellAttachment
	// Facing represents the horizontal direction of the Bell.
	Facing cube.Direction
	// Powered is true while the Bell receives redstone power. It rings when
	// power starts.
	Powered bool

	ringing       bool
	ticks         int
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
	b.ring(pos, tx, face)
	return true
}

// ring ports BellBlock.attemptToRing: the bell swings towards face.
func (b Bell) ring(pos cube.Pos, tx *world.Tx, face cube.Face) {
	b.ringing, b.ticks = true, 0
	b.ringDirection = face
	tx.SetBlock(pos, b, &world.SetOpts{DisableBlockUpdates: true, DisableLiquidDisplacement: true})
	tx.PlaySound(pos.Vec3Centre(), sound.BellRing{})
	// BellBlockEntity.updateEntities: the mobs within 32 blocks remember
	// hearing the bell.
	centre := pos.Vec3Centre()
	for e := range tx.EntitiesWithin(cube.Box(-48, -48, -48, 49, 49, 49).Translate(pos.Vec3())) {
		if h, ok := e.(interface{ HearBell(time int64) }); ok && e.Position().Sub(centre).LenSqr() < 32*32 {
			h.HearBell(tx.CurrentTick())
		}
	}
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

// NeighbourUpdateTick drops a Bell that lost its support and, as
// BellBlock.neighborChanged, rings it towards its facing when redstone power
// starts.
func (b Bell) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	switch b.Attach {
	case StandingBellAttachment():
		if !bellSupportSolid(tx, pos, cube.FaceDown) {
			breakBlock(b, pos, tx)
			return
		}
	case HangingBellAttachment():
		if !bellSupportSolid(tx, pos, cube.FaceUp) {
			breakBlock(b, pos, tx)
			return
		}
	case SideBellAttachment():
		if !bellSupportSolid(tx, pos, b.Facing.Face().Opposite()) {
			breakBlock(b, pos, tx)
			return
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
			return
		}
	}
	if powered := redstonePowered(pos, tx); powered != b.Powered {
		b.Powered = powered
		if powered {
			b.ring(pos, tx, b.Facing.Face())
			return
		}
		tx.SetBlock(pos, b, &world.SetOpts{DisableBlockUpdates: true, DisableLiquidDisplacement: true})
	}
}

// Tick advances the Ticks count of the Bell as BDS does: two a tick while
// ringing until it reaches 50 and the bell stops, one a tick while still,
// wrapping at 50.
func (b Bell) Tick(_ int64, pos cube.Pos, tx *world.Tx) {
	if b.ringing {
		if b.ticks += 2; b.ticks >= bellRingTicks {
			b.ringing, b.ticks = false, 0
		}
	} else if b.ticks++; b.ticks >= bellRingTicks {
		b.ticks = 0
	}
	tx.SetBlockEntity(pos, b)
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
		"toggle_bit": boolByte(b.Powered),
	}
}

// EncodeItem ...
func (Bell) EncodeItem() (name string, meta int16) {
	return "minecraft:bell", 0
}

// EncodeNBT ...
func (b Bell) EncodeNBT() map[string]any {
	return map[string]any{
		"id":                 "Bell",
		"BlockEntityVersion": int32(0),
		"Ringing":            boolByte(b.ringing),
		"Ticks":              int32(b.ticks),
		"Direction":          b.ringDirectionID(),
	}
}

// DecodeNBT ...
func (b Bell) DecodeNBT(data map[string]any) any {
	b.ringing = nbtconv.Bool(data, "Ringing")
	b.ticks = int(nbtconv.Int32(data, "Ticks"))
	b.ringDirection = cube.Face(255)
	if direction, ok := data["Direction"].(int32); ok {
		b.ringDirection = bellRingFace(direction)
	}
	return b
}

// SideClosed ...
func (Bell) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool {
	return false
}

func (b Bell) canRing(face cube.Face) bool {
	return face.Axis() != cube.Y && face.Axis() != b.Facing.Face().Axis()
}

// ringDirectionID returns the Direction the bell last swung towards as the
// Bedrock horizontal direction the block state uses, or 255 if it never rang.
func (b Bell) ringDirectionID() int32 {
	if b.ringDirection < cube.FaceNorth || b.ringDirection > cube.FaceEast {
		return 255
	}
	return int32(horizontalDirection(b.ringDirection.Direction()))
}

// bellRingFace returns the face of a Bedrock horizontal direction stored as
// Direction, or an invalid face for 255.
func bellRingFace(id int32) cube.Face {
	for _, d := range cube.Directions() {
		if int32(horizontalDirection(d)) == id {
			return d.Face()
		}
	}
	return cube.Face(255)
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
			bells = append(bells, Bell{Attach: a, Facing: d, Powered: true})
		}
	}
	return
}
