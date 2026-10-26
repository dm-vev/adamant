package item

import (
	"log/slog"
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// ArmourStand is an armour stand item. It can be placed to create an armour stand entity that can hold and display
// armour and other items.
type ArmourStand struct{}

// UseOnBlock places an armour stand entity as ArmorStandItem.useOn: not on
// the bottom of a block, standing on the floor of the placed position, which
// must be clear of blocks and entities, and turned to the nearest 45 degrees
// facing the player.
func (ArmourStand) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user User, ctx *UseContext) bool {
	create := tx.World().EntityRegistry().Config().ArmourStand
	if create == nil {
		slog.Default().Info("armour_stand: missing entity factory", "world", tx.World().Name())
		return false
	}
	if face == cube.FaceDown {
		return false
	}
	at := placeContextPos(tx, pos, face)
	spawnPos := mgl64.Vec3{float64(at[0]) + 0.5, float64(at[1]), float64(at[2]) + 0.5}
	// EntityTypes.ARMOR_STAND is 0.5 wide and 1.975 high.
	box := cube.Box(-0.25, 0, -0.25, 0.25, 1.975, 0.25).Translate(spawnPos)
	if !entityBoxClear(tx, box) {
		return false
	}
	yaw := math.Floor((float64(wrapDegrees(float32(user.Rotation().Yaw()-180)))+22.5)/45) * 45
	opts := stackSpawnOpts(user, world.EntitySpawnOpts{Position: spawnPos, Rotation: cube.Rotation{yaw, 0}})
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
