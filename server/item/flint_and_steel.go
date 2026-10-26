package item

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// FlintAndSteel is an item used to light blocks on fire.
type FlintAndSteel struct{}

// MaxCount ...
func (f FlintAndSteel) MaxCount() int {
	return 1
}

// DurabilityInfo ...
func (f FlintAndSteel) DurabilityInfo() DurabilityInfo {
	return DurabilityInfo{
		MaxDurability: 64,
		BrokenItem:    simpleItem(Stack{}),
	}
}

// ignitable represents a block that can be lit by a fire emitter, such as flint and steel.
type ignitable interface {
	// Ignite is called when the block is lit by flint and steel.
	Ignite(pos cube.Pos, tx *world.Tx, igniter world.Entity) bool
}

// UseOnBlock ...
func (f FlintAndSteel) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user User, ctx *UseContext) bool {
	if l, ok := tx.Block(pos).(ignitable); ok && l.Ignite(pos, tx, user) {
		ctx.DamageItem(1)
		return true
	}
	// FlintAndSteelItem.useOn: otherwise fire goes beside the clicked face
	// where BaseFireBlock.canBePlacedAt allows it.
	s := pos.Side(face)
	if !tx.PlaceFire(s) {
		return false
	}
	tx.PlaySound(s.Vec3Centre(), sound.Ignite{})
	if user != nil {
		tx.EmitGameEvent(world.GameEventBlockPlace, pos.Vec3Centre(), world.GameEventContext{Source: user})
	}
	ctx.DamageItem(1)
	return true
}

// EncodeItem ...
func (f FlintAndSteel) EncodeItem() (name string, meta int16) {
	return "minecraft:flint_and_steel", 0
}

// air returns an air block.
func air() world.Block {
	a, ok := world.BlockByName("minecraft:air", nil)
	if !ok {
		panic("could not find air block")
	}
	return a
}
