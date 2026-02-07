package item

import (
	"log/slog"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// ArmourStand is an armour stand item. It can be placed to create an armour stand entity that can hold and display
// armour and other items.
type ArmourStand struct{}

// UseOnBlock places an armour stand entity.
func (ArmourStand) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user User, ctx *UseContext) bool {
	create := tx.World().EntityRegistry().Config().ArmourStand
	if create == nil {
		slog.Default().Info("armour_stand: missing entity factory", "world", tx.World().Name())
		return false
	}

	spawnPos := pos.Side(face).Vec3Middle()
	opts := world.EntitySpawnOpts{Position: spawnPos, Rotation: user.Rotation().Neg()}
	tx.AddEntity(create(opts))
	ctx.SubtractFromCount(1)
	tx.PlaySound(spawnPos, sound.ArmourStandPlace{})
	return true
}

// EncodeItem ...
func (ArmourStand) EncodeItem() (name string, meta int16) {
	return "minecraft:armor_stand", 0
}

// MaxCount returns the maximum amount of armour stands that may be held in a single stack.
func (ArmourStand) MaxCount() int {
	return 16
}
