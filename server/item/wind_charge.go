package item

import (
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// WindCharge is a throwable item that bursts into a blast of wind on impact, launching nearby entities.
type WindCharge struct{}

// MaxCount ...
func (WindCharge) MaxCount() int {
	return 64
}

// Cooldown returns the delay applied between consecutive wind charge throws.
func (WindCharge) Cooldown() time.Duration {
	return time.Second / 2
}

// EncodeItem ...
func (WindCharge) EncodeItem() (name string, meta int16) {
	return "minecraft:wind_charge", 0
}

// Use launches the wind charge projectile from the user.
func (WindCharge) Use(tx *world.Tx, user User, ctx *UseContext) bool {
	create := tx.World().EntityRegistry().Config().WindCharge
	if create == nil {
		return false
	}

	dir := user.Rotation().Vec3()
	if dir.Len() == 0 {
		dir = mgl64.Vec3{0, 0, 1}
	}
	dir = dir.Normalize()

	velocity := dir.Mul(2.25)
	// Apply a slight randomised cone spread that mirrors the in-game behaviour.
	spread := 0.015
	velocity = velocity.Add(mgl64.Vec3{
		(rand.Float64()*2 - 1) * spread,
		(rand.Float64()*2 - 1) * spread,
		(rand.Float64()*2 - 1) * spread,
	})

	pos := eyePosition(user).Add(dir.Mul(0.25))
	opts := world.EntitySpawnOpts{Position: pos, Velocity: velocity}
	tx.AddEntity(create(opts, user))
	tx.PlaySound(user.Position(), sound.WindChargeShoot{})

	ctx.SubtractFromCount(1)
	return true
}
