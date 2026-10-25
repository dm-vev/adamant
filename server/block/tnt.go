package block

import (
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
)

// TNT is an explosive block that can be primed to generate an explosion.
type TNT struct {
	solid
}

var _ world.RedstonePowerAction = TNT{}

func (TNT) RedstoneNonConductive() {}

// RedstonePowerAction primes TNT when it first receives redstone power.
func (t TNT) RedstonePowerAction(pos cube.Pos, tx *world.Tx, oldPower, newPower int) {
	if oldPower > 0 || newPower == 0 {
		return
	}
	t.Ignite(pos, tx, nil)
}

// ProjectileHit ...
func (t TNT) ProjectileHit(pos cube.Pos, tx *world.Tx, e world.Entity, _ cube.Face) {
	// TntBlock.onProjectileHit: a burning projectile that may change
	// blocks primes it.
	if projectileOnFire(e) && projectileMayInteract(tx, e) {
		t.Ignite(pos, tx, e)
	}
}

// Activate ...
func (t TNT) Activate(pos cube.Pos, _ cube.Face, tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	held, _ := u.HeldItems()
	if _, ok := held.Enchantment(enchantment.FireAspect); ok {
		t.Ignite(pos, tx, u)
		ctx.DamageItem(1)
		return true
	}
	return false
}

// Ignite primes the TNT unless the tnt_explodes gamerule is off.
func (t TNT) Ignite(pos cube.Pos, tx *world.Tx, owner world.Entity) bool {
	if !tx.World().GameRuleBool("tnt_explodes") {
		return false
	}
	spawnTnt(pos, tx, time.Second*4, owner)
	return true
}

// Explode primes TNT caught in an explosion with Java's shortened random fuse.
func (t TNT) Explode(src world.ExplosionSource, pos cube.Pos, tx *world.Tx) {
	if !tx.World().GameRuleBool("tnt_explodes") {
		return
	}
	spawnTnt(pos, tx, time.Second/2+time.Duration(rand.IntN(int(time.Second+time.Second/2))), world.ExplosionCausingEntity(src, tx))
}

// BreakInfo ...
func (t TNT) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, oneOf(t))
}

// FlammabilityInfo ...
func (t TNT) FlammabilityInfo() FlammabilityInfo {
	return newFlammabilityInfo(15, 100, true)
}

// EncodeItem ...
func (t TNT) EncodeItem() (name string, meta int16) {
	return "minecraft:tnt", 0
}

// EncodeBlock ...
func (t TNT) EncodeBlock() (name string, properties map[string]interface{}) {
	return "minecraft:tnt", map[string]interface{}{"explode_bit": false}
}

// spawnTnt creates a new TNT entity at the given position with the given fuse duration.
func spawnTnt(pos cube.Pos, tx *world.Tx, fuse time.Duration, owner world.Entity) {
	tx.PlaySound(pos.Vec3Centre(), sound.TNT{})
	tx.SetBlock(pos, nil, nil)
	opts := world.EntitySpawnOpts{Position: pos.Vec3Centre()}
	tx.AddEntity(tx.World().EntityRegistry().Config().TNT(opts, fuse, owner))
}
