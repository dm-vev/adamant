package item

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// String is a crafting material dropped by spiders and found in various structures.
type String struct{}

// MaxCount ...
func (String) MaxCount() int {
	return 64
}

// EncodeItem ...
func (String) EncodeItem() (name string, meta int16) {
	return "minecraft:string", 0
}

// UseOnBlock places tripwire when the string is used on a block.
func (String) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, _ User, ctx *UseContext) bool {
	placePos := pos
	if !replaceableWith(tx.Block(pos), tripwireBlock()) {
		placePos = pos.Side(face)
	}
	if placePos.OutOfBounds(tx.Range()) {
		return false
	}
	tw := tripwireBlock()
	if !replaceableWith(tx.Block(placePos), tw) {
		return false
	}
	suspended := !tx.Block(placePos.Side(cube.FaceDown)).Model().FaceSolid(placePos.Side(cube.FaceDown), cube.FaceUp, tx)
	twProps := map[string]any{
		"attached_bit":  uint8(0),
		"disarmed_bit":  uint8(0),
		"powered_bit":   uint8(0),
		"suspended_bit": boolByte(suspended),
	}
	placed, ok := world.BlockByName("minecraft:trip_wire", twProps)
	if !ok {
		return false
	}
	tx.SetBlock(placePos, placed, nil)
	ctx.SubtractFromCount(1)
	return true
}

func tripwireBlock() world.Block {
	b, ok := world.BlockByName("minecraft:trip_wire", nil)
	if !ok {
		panic("could not find tripwire block")
	}
	return b
}
