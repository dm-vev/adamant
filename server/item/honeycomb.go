package item

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// Honeycomb is an item obtained from bee nests and beehives.
type Honeycomb struct{}

// UseOnBlock waxes copper blocks and signs, as HoneycombItem.useOn and its
// tryApplyToSign.
func (Honeycomb) UseOnBlock(pos cube.Pos, _ cube.Face, _ mgl64.Vec3, tx *world.Tx, user User, ctx *UseContext) bool {
	if wa, ok := tx.Block(pos).(waxable); ok {
		if res, ok := wa.Wax(pos, user.Position()); ok {
			tx.SetBlock(pos, res, nil)
			// Level event 3003: the wax-on particles and sound.
			tx.PlaySound(pos.Vec3(), sound.SignWaxed{})
			tx.EmitGameEvent(world.GameEventBlockChange, pos.Vec3Centre(), world.GameEventContext{Source: user, AffectedState: res})
			ctx.SubtractFromCount(1)
			return true
		}
	}
	return false
}

// waxable represents a block that may be waxed.
type waxable interface {
	// Wax uses an ink sac on the block, returning the resulting block and a bool specifying if waxing the block was
	// successful.
	Wax(pos cube.Pos, userPos mgl64.Vec3) (world.Block, bool)
}

// EncodeItem ...
func (Honeycomb) EncodeItem() (name string, meta int16) {
	return "minecraft:honeycomb", 0
}
