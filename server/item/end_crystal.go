package item

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// EndCrystal is an item that may be placed to spawn an end crystal entity.
type EndCrystal struct{}

// UseOnBlock places the end crystal entity above the clicked block if possible.
func (EndCrystal) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, _ User, ctx *UseContext) bool {
	if face != cube.FaceUp {
		return false
	}

	base := tx.Block(pos)
	name, _ := base.EncodeBlock()
	showBase := true
	switch name {
	case "minecraft:bedrock":
		showBase = false
	case "minecraft:obsidian":
		// Allowed without changes.
	default:
		return false
	}

	above := pos.Side(face)
	aboveName, _ := tx.Block(above).EncodeBlock()
	if aboveName != "minecraft:air" {
		return false
	}
	if _, liquid := tx.Liquid(above); liquid {
		return false
	}

	create := tx.World().EntityRegistry().Config().EndCrystal
	if create == nil {
		return false
	}
	opts := world.EntitySpawnOpts{Position: above.Vec3Centre()}
	tx.AddEntity(create(opts, showBase))

	ctx.SubtractFromCount(1)
	return true
}

// EncodeItem returns the ID and meta of the end crystal item.
func (EndCrystal) EncodeItem() (string, int16) {
	return "minecraft:end_crystal", 0
}
