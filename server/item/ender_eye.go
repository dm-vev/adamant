package item

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/portal"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// EnderEye is the item used to fill End portal frames and locate strongholds.
type EnderEye struct{}

// EncodeItem ...
func (EnderEye) EncodeItem() (name string, meta int16) {
	return "minecraft:ender_eye", 0
}

// MaxCount ...
func (EnderEye) MaxCount() int {
	return 64
}

// endPortalFrame is implemented by block.EndPortalFrame, which cannot be imported here directly.
type endPortalFrame interface {
	InsertEndPortalEye(pos cube.Pos, tx *world.Tx) bool
}

// UseOnBlock fills the targeted End portal frame with an Eye of Ender, activating the portal if this completes the
// twelve-frame ring.
func (EnderEye) UseOnBlock(pos cube.Pos, _ cube.Face, _ mgl64.Vec3, tx *world.Tx, _ User, ctx *UseContext) bool {
	f, ok := tx.Block(pos).(endPortalFrame)
	if !ok {
		return false
	}
	if !f.InsertEndPortalEye(pos, tx) {
		return false
	}
	tx.PlaySound(pos.Vec3Centre(), sound.EnderEyePlaced{})
	if portal.ActivateEndPortal(tx, pos) {
		tx.PlayGlobalSound(pos.Vec3Centre(), sound.EndPortalCreated{})
	}
	ctx.SubtractFromCount(1)
	return true
}

// Use throws an eye of ender towards the nearest stronghold, as Java
// EnderEyeItem.use. Without a located stronghold the item is not consumed.
func (EnderEye) Use(tx *world.Tx, user User, ctx *UseContext) bool {
	create := tx.World().EntityRegistry().Config().EyeOfEnder
	if create == nil || tx.World().Dimension() != world.Overworld {
		return false
	}
	loc, ok, err := tx.World().FindNearestMapFeature("minecraft:eye_of_ender_located", cube.PosFromVec3(user.Position()), 100, false)
	if err != nil || !ok {
		return false
	}
	// Java spawns the eye at the user's mid-height (getY(0.5)).
	pos := user.Position().Add(mgl64.Vec3{0, .9})
	tx.AddEntity(create(world.EntitySpawnOpts{Position: pos}, loc.Position.Vec3()))
	tx.EmitGameEvent(world.GameEventProjectileShoot, pos, world.GameEventContext{Source: user})
	// Bedrock has no ender_eye.launch sound and plays the throw sound.
	tx.PlaySound(pos, sound.ItemThrow{})
	if creative, ok := user.(interface{ GameMode() world.GameMode }); !ok || !creative.GameMode().CreativeInventory() {
		ctx.SubtractFromCount(1)
	}
	return true
}
