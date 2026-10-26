package item

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// FireCharge is an item that can be used to place fire when used on a block, or shot from a dispenser to create a small
// fireball.
type FireCharge struct{}

// EncodeItem ...
func (f FireCharge) EncodeItem() (name string, meta int16) {
	return "minecraft:fire_charge", 0
}

// UseOnBlock ...
func (f FireCharge) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user User, ctx *UseContext) bool {
	if l, ok := tx.Block(pos).(ignitable); ok && l.Ignite(pos, tx, user) {
		ctx.SubtractFromCount(1)
		tx.PlaySound(pos.Vec3Centre(), sound.FireCharge{})
		return true
	}
	// FireChargeItem.useOn: otherwise fire goes beside the clicked face where
	// BaseFireBlock.canBePlacedAt allows it.
	s := pos.Side(face)
	if !tx.PlaceFire(s) {
		return false
	}
	tx.PlaySound(s.Vec3Centre(), sound.FireCharge{})
	if user != nil {
		tx.EmitGameEvent(world.GameEventBlockPlace, s.Vec3Centre(), world.GameEventContext{Source: user})
	}
	ctx.SubtractFromCount(1)
	return true
}
