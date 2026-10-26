package item

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// Painting is an item used to hang a randomly chosen canvas on a wall.
type Painting struct{}

// UseOnBlock ports HangingEntityItem.useOn: paintings hang only on the
// horizontal sides of blocks, in the block in front of the clicked face.
func (Painting) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user User, ctx *UseContext) bool {
	if face == cube.FaceUp || face == cube.FaceDown {
		return false
	}
	create := tx.World().EntityRegistry().Config().Painting
	if create == nil {
		return false
	}
	h := create(tx, pos.Side(face), face.Direction())
	if h == nil {
		return false
	}
	placed := tx.AddEntity(h)
	tx.PlaySound(pos.Side(face).Vec3Centre(), sound.HangingPlace{})
	if user != nil {
		tx.EmitGameEvent(world.GameEventEntityPlace, placed.Position(), world.GameEventContext{Source: user})
	}
	ctx.SubtractFromCount(1)
	return true
}

// EncodeItem ...
func (Painting) EncodeItem() (string, int16) { return "minecraft:painting", 0 }
