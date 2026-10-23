package entity

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/world"
)

type (
	// AttackDamageSource is used for damage caused by other entities, for
	// example when a player attacks another player.
	AttackDamageSource struct {
		// Attacker holds the attacking entity. The entity may be a player or
		// any other entity.
		Attacker world.Entity
	}

	// SpearDamageSource is used for Java spear kinetic and thrust attacks.
	SpearDamageSource struct {
		AttackDamageSource
	}

	// MaceSmashDamageSource is used for mace smash attacks that scale armour
	// effectiveness based on the Breach enchantment.
	MaceSmashDamageSource struct {
		AttackDamageSource
		ArmourMultiplier float64
	}

	// VoidDamageSource is used for damage caused by an entity being in the
	// void.
	VoidDamageSource struct{}

	// SuffocationDamageSource is used for damage caused by an entity
	// suffocating in a block.
	SuffocationDamageSource struct{}

	// DrowningDamageSource is used for damage caused by an entity drowning in
	// water.
	DrowningDamageSource struct{}

	// FallDamageSource is used for damage caused by falling.
	FallDamageSource struct{}

	// GlideDamageSource is used for damage caused by gliding into a block.
	GlideDamageSource struct{}

	// LightningDamageSource is used for damage caused by being struck by
	// lightning.
	LightningDamageSource struct{ BoltUUID string }

	// ProjectileDamageSource is used for damage caused by a projectile.
	ProjectileDamageSource struct {
		// Projectile and Owner are the world.Entity that dealt the damage and
		// the one that fired the projectile respectively.
		Projectile, Owner world.Entity
	}

	// ExplosionDamageSource is used for damage caused by an explosion.
	ExplosionDamageSource struct {
		// Source is the source of the explosion that dealt the damage.
		Source world.ExplosionSource
	}
	// WindChargeDamageSource is used for damage caused by a wind charge burst.
	WindChargeDamageSource struct{}
	// SplashPotionDamageSource is the indirect magic damage a splash of water
	// deals to entities that water hurts.
	SplashPotionDamageSource struct {
		// Projectile is the thrown potion and Owner the entity that threw
		// it, if any.
		Projectile, Owner world.Entity
	}
)

func (FallDamageSource) ReducedByArmour() bool     { return false }
func (FallDamageSource) ReducedByResistance() bool { return true }
func (FallDamageSource) Fire() bool                { return false }
func (FallDamageSource) AffectedByEnchantment(e item.EnchantmentType) bool {
	return e == enchantment.FeatherFalling
}
func (FallDamageSource) IgnoreTotem() bool                { return false }
func (GlideDamageSource) ReducedByArmour() bool           { return false }
func (GlideDamageSource) ReducedByResistance() bool       { return true }
func (GlideDamageSource) Fire() bool                      { return false }
func (GlideDamageSource) IgnoreTotem() bool               { return false }
func (LightningDamageSource) ReducedByArmour() bool       { return true }
func (LightningDamageSource) ReducedByResistance() bool   { return true }
func (LightningDamageSource) Fire() bool                  { return false }
func (LightningDamageSource) IgnoreTotem() bool           { return false }
func (AttackDamageSource) ReducedByArmour() bool          { return true }
func (AttackDamageSource) ReducedByResistance() bool      { return true }
func (AttackDamageSource) Fire() bool                     { return false }
func (AttackDamageSource) IgnoreTotem() bool              { return false }
func (VoidDamageSource) ReducedByResistance() bool        { return false }
func (VoidDamageSource) ReducedByArmour() bool            { return false }
func (VoidDamageSource) Fire() bool                       { return false }
func (VoidDamageSource) IgnoreTotem() bool                { return true }
func (SuffocationDamageSource) ReducedByResistance() bool { return false }
func (SuffocationDamageSource) ReducedByArmour() bool     { return false }
func (SuffocationDamageSource) Fire() bool                { return false }
func (SuffocationDamageSource) IgnoreTotem() bool         { return false }
func (DrowningDamageSource) ReducedByResistance() bool    { return false }
func (DrowningDamageSource) ReducedByArmour() bool        { return false }
func (DrowningDamageSource) Fire() bool                   { return false }
func (DrowningDamageSource) IgnoreTotem() bool            { return false }
func (ProjectileDamageSource) ReducedByResistance() bool  { return true }
func (ProjectileDamageSource) ReducedByArmour() bool      { return true }
func (ProjectileDamageSource) Fire() bool                 { return false }
func (ProjectileDamageSource) AffectedByEnchantment(e item.EnchantmentType) bool {
	return e == enchantment.ProjectileProtection
}
func (ProjectileDamageSource) IgnoreTotem() bool        { return false }
func (ExplosionDamageSource) ReducedByResistance() bool { return true }
func (ExplosionDamageSource) ReducedByArmour() bool     { return true }
func (ExplosionDamageSource) Fire() bool                { return false }
func (ExplosionDamageSource) AffectedByEnchantment(e item.EnchantmentType) bool {
	return e == enchantment.BlastProtection
}
func (ExplosionDamageSource) IgnoreTotem() bool { return false }

func (WindChargeDamageSource) ReducedByResistance() bool { return true }
func (WindChargeDamageSource) ReducedByArmour() bool     { return true }
func (WindChargeDamageSource) Fire() bool                { return false }
func (WindChargeDamageSource) AffectedByEnchantment(e item.EnchantmentType) bool {
	return e == enchantment.BlastProtection
}
func (WindChargeDamageSource) IgnoreTotem() bool { return false }

func (SplashPotionDamageSource) ReducedByResistance() bool { return true }
func (SplashPotionDamageSource) ReducedByArmour() bool     { return false }
func (SplashPotionDamageSource) Fire() bool                { return false }
func (SplashPotionDamageSource) IgnoreTotem() bool         { return false }

// ArmourEffectivenessMultiplier returns the armour scaling applied for the smash attack.
func (s MaceSmashDamageSource) ArmourEffectivenessMultiplier() float64 {
	if s.ArmourMultiplier <= 0 {
		return 0
	}
	return s.ArmourMultiplier
}

// bodyArmour is armour an animal wears on its body, such as horse or
// nautilus armour.
type bodyArmour interface {
	DefencePoints() float64
	Toughness() float64
	KnockBackResistance() float64
}

// damageAfterBodyArmour ports CombatRules.getDamageAfterAbsorb for the body
// armour an animal wears: damage armour reduces is lowered by its defence,
// less so the harder the hit unless the armour is tough.
func damageAfterBodyArmour(d float64, src world.DamageSource, armour item.Stack) float64 {
	a, ok := armour.Item().(bodyArmour)
	if armour.Empty() || !ok {
		return d
	}
	return damageAfterArmour(d, src, a.DefencePoints(), a.Toughness())
}

// damageAfterArmour ports CombatRules.getDamageAfterAbsorb: damage armour
// reduces is lowered by its defence points, less so the harder the hit
// unless the armour is tough.
func damageAfterArmour(d float64, src world.DamageSource, defence, toughness float64) float64 {
	if defence <= 0 || !src.ReducedByArmour() {
		return d
	}
	points, t := float32(defence), float32(2+toughness/4)
	real := min(max(points-float32(d)/t, points*0.2), 20)
	return d * float64(1-real/25)
}
