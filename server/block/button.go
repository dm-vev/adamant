package block

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Button is a redstone component that emits a short pulse.
type Button struct {
	transparent
	empty

	Type    ButtonType
	Facing  cube.Face
	Pressed bool
}

// BreakInfo ...
func (b Button) BreakInfo() BreakInfo {
	return newBreakInfo(0.5, alwaysHarvestable, nothingEffective, oneOf(b))
}

// UseOnBlock ...
func (b Button) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, face, used := firstReplaceable(tx, pos, face, b)
	if !used {
		return false
	}
	if !tx.Block(pos.Side(face.Opposite())).Model().FaceSolid(pos.Side(face.Opposite()), face, tx) {
		return false
	}
	b.Facing = face
	place(tx, pos, b, user, ctx)
	return placed(ctx)
}

// Activate ...
func (b Button) Activate(pos cube.Pos, _ cube.Face, tx *world.Tx, _ item.User, _ *item.UseContext) bool {
	if b.Pressed {
		return false
	}
	b.Pressed = true
	tx.SetBlock(pos, b, nil)
	playRedstoneSwitchSound(pos, tx, b, true)
	tx.ScheduleBlockUpdate(pos, b, gameTicks(b.Type.pressTicks()))
	tx.DoBlockUpdatesAround(pos)
	tx.DoBlockUpdatesAround(pos.Side(b.Facing.Opposite()))
	return true
}

// NeighbourUpdateTick ...
func (b Button) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !tx.Block(pos.Side(b.Facing.Opposite())).Model().FaceSolid(pos.Side(b.Facing.Opposite()), b.Facing, tx) {
		breakBlock(b, pos, tx)
	}
}

// ScheduledTick ...
func (b Button) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	if !b.Pressed {
		return
	}
	if b.Type.pressTicks() == 30 && b.hasActivatingProjectile(pos, tx) {
		tx.ScheduleBlockUpdate(pos, b, gameTicks(b.Type.pressTicks()))
		return
	}
	b.Pressed = false
	tx.SetBlock(pos, b, nil)
	playRedstoneSwitchSound(pos, tx, b, false)
	tx.DoBlockUpdatesAround(pos)
	tx.DoBlockUpdatesAround(pos.Side(b.Facing.Opposite()))
}

// EntityInside checks the native arrow/trident query whenever an entity
// overlaps an unpressed wooden button. The entering entity need not itself
// be the activating projectile.
func (b Button) EntityInside(pos cube.Pos, tx *world.Tx, _ world.Entity) {
	if !b.Pressed && b.Type.pressTicks() == 30 && b.hasActivatingProjectile(pos, tx) {
		b.Activate(pos, b.Facing, tx, nil, nil)
	}
}

// ProjectileBBox follows ButtonBlock::getCollisionShape: arrows and thrown
// tridents collide with the unpressed button outline, even while pressed.
// Other projectiles and ordinary entities pass through it.
func (b Button) ProjectileBBox(_ cube.Pos, _ world.BlockSource, e world.Entity) []cube.BBox {
	if !buttonActivatingProjectile(e) {
		return nil
	}
	return []cube.BBox{b.projectileBox()}
}

func buttonActivatingProjectile(e world.Entity) bool {
	switch e.H().Type().EncodeEntity() {
	case "minecraft:arrow", "minecraft:thrown_trident":
		return true
	}
	return false
}

func (b Button) hasActivatingProjectile(pos cube.Pos, tx *world.Tx) bool {
	box := b.projectileBox().Translate(pos.Vec3())
	// EntitiesWithin filters origins, not bounding boxes. Expand the query
	// for arrow/trident dimensions, then test their actual boxes. This query
	// visits available chunks only.
	for e := range tx.EntitiesWithin(box.Grow(1)) {
		if !buttonActivatingProjectile(e) {
			continue
		}
		other := e.H().Type().BBox(e).Translate(e.Position())
		lo, hi, otherLo, otherHi := box.Min(), box.Max(), other.Min(), other.Max()
		// Native queries include touching faces. A resting arrow on a floor
		// button has its lower Y exactly equal to the button's upper Y.
		if lo[0] <= otherHi[0] && hi[0] >= otherLo[0] &&
			lo[1] <= otherHi[1] && hi[1] >= otherLo[1] &&
			lo[2] <= otherHi[2] && hi[2] >= otherLo[2] {
			return true
		}
	}
	return false
}

// projectileBox is the fixed query/collision outline used by the native
// button, independent of its animated pressed depth.
func (b Button) projectileBox() cube.BBox {
	switch b.Facing {
	case cube.FaceDown:
		return cube.Box(5.0/16, 14.0/16, 6.0/16, 11.0/16, 1, 10.0/16)
	case cube.FaceUp:
		return cube.Box(5.0/16, 0, 6.0/16, 11.0/16, 2.0/16, 10.0/16)
	case cube.FaceNorth:
		return cube.Box(5.0/16, 6.0/16, 14.0/16, 11.0/16, 10.0/16, 1)
	case cube.FaceSouth:
		return cube.Box(5.0/16, 6.0/16, 0, 11.0/16, 10.0/16, 2.0/16)
	case cube.FaceWest:
		return cube.Box(14.0/16, 6.0/16, 5.0/16, 1, 10.0/16, 11.0/16)
	case cube.FaceEast:
		return cube.Box(0, 6.0/16, 5.0/16, 2.0/16, 10.0/16, 11.0/16)
	}
	return cube.BBox{}
}

func (b Button) RedstonePower(cube.Pos, *world.Tx, cube.Face) int {
	if b.Pressed {
		return 15
	}
	return 0
}

func (b Button) RedstoneStrongPower(_ cube.Pos, _ *world.Tx, face cube.Face) int {
	if !b.Pressed {
		return 0
	}
	if face == b.Facing.Opposite() {
		return 15
	}
	return 0
}

// EncodeItem ...
func (b Button) EncodeItem() (name string, meta int16) {
	return "minecraft:" + b.Type.blockName(), 0
}

// EncodeBlock ...
func (b Button) EncodeBlock() (string, map[string]any) {
	return "minecraft:" + b.Type.blockName(), map[string]any{"button_pressed_bit": boolByte(b.Pressed), "facing_direction": int32(b.Facing)}
}

func allButtons() (buttons []world.Block) {
	for _, t := range ButtonTypes() {
		for _, face := range cube.Faces() {
			buttons = append(buttons, Button{Type: t, Facing: face, Pressed: false})
			buttons = append(buttons, Button{Type: t, Facing: face, Pressed: true})
		}
	}
	return
}
