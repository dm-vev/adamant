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
	// Powered is the native toggle_bit, flipped by each accepted
	// ring. Input power history belongs to the actor and circuit separately.
	Powered bool

	ringing         bool
	ticks           int
	ringDirection   cube.Face
	redstonePowered bool
	ringUpdate      bool
}

// BreakInfo ...
func (b Bell) BreakInfo() BreakInfo {
	return newBreakInfo(5, alwaysHarvestable, pickaxeEffective, oneOf(b)).withBlastResistance(5)
}

// Activate ...
func (b Bell) Activate(pos cube.Pos, _ cube.Face, tx *world.Tx, user item.User, _ *item.UseContext) bool {
	// BellBlock::use selects the player's horizontal look direction, not
	// the clicked face, before applying the attachment's direction gate.
	return b.Ring(pos, tx, user.Rotation().Direction().Opposite().Face())
}

// ProjectileHit ...
func (b Bell) ProjectileHit(pos cube.Pos, tx *world.Tx, projectile world.Entity, _ cube.Face) {
	// BellBlock::onProjectileHit excludes wind charges, whose activating
	// attack has a separate path. Other hits ring along the bell's facing,
	// without the player-use axis gate or the projectile's impact face.
	if projectile != nil {
		switch projectile.H().Type().EncodeEntity() {
		case "minecraft:wind_charge_projectile", "minecraft:breeze_wind_charge", "minecraft:breeze_wind_charge_projectile":
			return
		}
	}
	b.ring(pos, tx, b.Facing.Face(), false)
}

// Ring starts or refreshes the Bell ring state in the direction of face.
func (b Bell) Ring(pos cube.Pos, tx *world.Tx, face cube.Face) bool {
	if !b.canRing(face) {
		return false
	}
	b.ring(pos, tx, face, false)
	return true
}

// ring ports BellBlock.attemptToRing: the bell swings towards face.
func (b Bell) ring(pos cube.Pos, tx *world.Tx, face cube.Face, redstone bool) {
	b.ringUpdate = redstone
	b.Powered = !b.Powered
	b.ringing, b.ticks = true, 0
	b.ringDirection = face
	if redstone {
		tx.PlaySound(pos.Vec3(), sound.BellRing{})
	}
	tx.SetBlock(pos, b, &world.SetOpts{DisableBlockUpdates: true, DisableLiquidDisplacement: true})
	if redstone {
		viewers := tx.Viewers(pos.Vec3())
		for _, viewer := range viewers {
			viewer.ViewBlockAction(pos, BellRingAction{Bell: b})
		}
		tx.ReleaseViewers(viewers)
	} else {
		// Native player use sends the changed state and one actor snapshot
		// before its sound, unlike the actor-pass redstone notification pair.
		tx.PlaySound(pos.Vec3(), sound.BellRing{AfterBlock: true})
	}
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

// NeighbourChanged drops a Bell that lost its support and, as
// BellBlock.neighborChanged, updates its attachment. The actor samples its
// native scene consumer when deciding whether to ring.
func (b Bell) NeighbourChanged(pos, changed cube.Pos, tx *world.Tx) {
	// Native command states are not checked by their own placement. Observe
	// actual loaded neighbour changes immediately, without replaying stale
	// deferred notifications created before this bell was installed.
	if pos == changed {
		return
	}
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
		if bellSupportSolid(tx, pos, b.Facing.Face()) {
			b.Attach = MultipleBellAttachment()
			tx.SetBlock(pos, b, &world.SetOpts{DisableBlockUpdates: true, DisableLiquidDisplacement: true})
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
}

// Tick advances the Ticks count of the Bell as BDS does: two a tick while
// ringing until it reaches 50 and the bell stops, one a tick while still,
// wrapping at 50.
func (b Bell) Tick(current int64, pos cube.Pos, tx *world.Tx) {
	b.ringUpdate = false
	if b.ringing {
		if b.ticks += 2; b.ticks >= bellRingTicks {
			b.ringing, b.ticks = false, 0
		}
	} else if b.ticks++; b.ticks >= bellRingTicks {
		b.ticks = 0
	}
	// BellBlockActor::tick advances animation every GT, but tests consumer
	// strength only on the dimension-counter gate (0x2db850e..0x2db851a).
	if current&1 == 0 {
		power, _ := tx.Redstone().ConsumerPower(pos)
		powered := power > 0
		wasPowered := b.redstonePowered
		b.redstonePowered = powered
		if powered && !wasPowered {
			b.ring(pos, tx, b.Facing.Face(), true)
			return
		}
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
	b.redstonePowered = false
	b.ringUpdate = false
	b.ringing = nbtconv.Bool(data, "Ringing")
	b.ticks = int(nbtconv.Int32(data, "Ticks"))
	b.ringDirection = cube.Face(255)
	if direction, ok := data["Direction"].(int32); ok {
		b.ringDirection = bellRingFace(direction)
	}
	return b
}

// UpdateNBT leaves the two redstone-ring actor snapshots to BellRingAction.
// This also keeps their order if a viewer flushes the ordinary state update
// before receiving that action. Chunk/save encoding still uses EncodeNBT.
func (b Bell) UpdateNBT() map[string]any {
	if b.ringUpdate {
		return nil
	}
	return b.EncodeNBT()
}

// SideClosed ...
func (Bell) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool {
	return false
}

func (b Bell) canRing(face cube.Face) bool {
	if face.Axis() == cube.Y {
		return false
	}
	switch b.Attach {
	case StandingBellAttachment():
		return face.Axis() == b.Facing.Face().Axis()
	case HangingBellAttachment():
		return true
	case SideBellAttachment(), MultipleBellAttachment():
		return face.Axis() != b.Facing.Face().Axis()
	}
	return false
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
