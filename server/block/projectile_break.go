package block

import (
	"github.com/df-mc/dragonfly/server/world"
)

// impactProjectiles is Java's #impact_projectiles entity tag by Bedrock
// identifier.
var impactProjectiles = map[string]bool{
	"minecraft:arrow": true, "minecraft:fireworks_rocket": true, "minecraft:snowball": true,
	"minecraft:fireball": true, "minecraft:small_fireball": true, "minecraft:egg": true,
	"minecraft:thrown_trident": true, "minecraft:dragon_fireball": true, "minecraft:wither_skull": true,
	"minecraft:wither_skull_dangerous": true, "minecraft:wind_charge_projectile": true,
	"minecraft:breeze_wind_charge": true,
}

// projectileOwner resolves the loaded owner of a projectile, if any.
func projectileOwner(tx *world.Tx, projectile world.Entity) world.Entity {
	var h *world.EntityHandle
	switch p := projectile.(type) {
	case interface{ ProjectileOwner() *world.EntityHandle }:
		h = p.ProjectileOwner()
	case interface{ Owner() *world.EntityHandle }:
		h = p.Owner()
	}
	if h == nil {
		return nil
	}
	owner, _ := h.Entity(tx)
	return owner
}

// projectileMayInteract ports Projectile.mayInteract: unowned projectiles
// may always interact, player-owned ones act for their owner, and mob-owned
// ones need mob_griefing.
func projectileMayInteract(tx *world.Tx, projectile world.Entity) bool {
	owner := projectileOwner(tx, projectile)
	if owner == nil {
		return true
	}
	if owner.H().Type().EncodeEntity() == "minecraft:player" {
		return true
	}
	return tx.World().GameRuleBool("mob_griefing")
}

// projectileMayBreak ports Projectile.mayBreak: impact projectiles may break
// fragile blocks while projectiles_can_break_blocks is on, unless shot by a
// player in adventure mode.
func projectileMayBreak(tx *world.Tx, projectile world.Entity) bool {
	if !impactProjectiles[projectile.H().Type().EncodeEntity()] || !tx.World().GameRuleBool("projectiles_can_break_blocks") {
		return false
	}
	if mode, ok := projectileOwner(tx, projectile).(interface{ GameMode() world.GameMode }); ok && mode.GameMode() == world.GameModeAdventure {
		return false
	}
	return true
}
