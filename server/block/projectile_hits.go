package block

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
)

// projectileOnFire reports if a projectile burns, as Entity.isOnFire.
func projectileOnFire(e world.Entity) bool {
	f, ok := e.(flammableEntity)
	return ok && f.OnFireDuration() > 0
}

// ProjectileHit ports CampfireBlock.onProjectileHit: a burning projectile
// that may change blocks lights an unlit, dry campfire.
func (c Campfire) ProjectileHit(pos cube.Pos, tx *world.Tx, e world.Entity, _ cube.Face) {
	if !c.Extinguished || !projectileOnFire(e) || !projectileMayInteract(tx, e) {
		return
	}
	if _, wet := tx.Liquid(pos); wet {
		return
	}
	c.Extinguished = false
	tx.SetBlock(pos, c, nil)
}

// ProjectileHit ports AbstractCandleBlock.onProjectileHit: a burning
// projectile lights unlit candles out of water.
func (c Candle) ProjectileHit(pos cube.Pos, tx *world.Tx, e world.Entity, _ cube.Face) {
	if c.Lit || !projectileOnFire(e) {
		return
	}
	if _, wet := tx.Liquid(pos); wet {
		return
	}
	c.Lit = true
	tx.SetBlock(pos, c, nil)
	tx.PlaySound(pos.Vec3Centre(), sound.Ignite{})
}

// ProjectileHit ports AbstractCandleBlock.onProjectileHit for the candle of
// a cake.
func (c CandleCake) ProjectileHit(pos cube.Pos, tx *world.Tx, e world.Entity, _ cube.Face) {
	if c.Lit || !projectileOnFire(e) {
		return
	}
	c.Lit = true
	tx.SetBlock(pos, c, nil)
	tx.PlaySound(pos.Vec3Centre(), sound.Ignite{})
}

// amethystChime ports AmethystBlock.onProjectileHit: amethyst a projectile
// hits chimes.
func amethystChime(pos cube.Pos, tx *world.Tx) {
	tx.PlaySound(pos.Vec3Centre(), sound.Custom{Name: "block.amethyst_block.chime", Volume: 1, Pitch: .5 + float64(rand.Float32()*1.2)})
}

// ProjectileHit chimes, as AmethystBlock.onProjectileHit.
func (Amethyst) ProjectileHit(pos cube.Pos, tx *world.Tx, _ world.Entity, _ cube.Face) {
	amethystChime(pos, tx)
}

// ProjectileHit chimes, as AmethystBlock.onProjectileHit.
func (BuddingAmethyst) ProjectileHit(pos cube.Pos, tx *world.Tx, _ world.Entity, _ cube.Face) {
	amethystChime(pos, tx)
}

// ProjectileHit chimes, as AmethystBlock.onProjectileHit.
func (AmethystCluster) ProjectileHit(pos cube.Pos, tx *world.Tx, _ world.Entity, _ cube.Face) {
	amethystChime(pos, tx)
}
