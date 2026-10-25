package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// windGust ports ServerExplosion.hurtEntities for the explosions of wind
// charges, the Wind Charged effect and Wind Burst, which hurt nothing and
// only push entities away. source is the entity exploding, which is not
// pushed. Items and armour stands are pushed only if blocklike is set, as
// Explosion.shouldAffectBlocklikeEntities.
func windGust(tx *world.Tx, source *world.EntityHandle, center mgl64.Vec3, radius, knockback float32, blocklike bool) {
	if radius < 1e-5 {
		return
	}
	doubleRadius := float64(radius * 2)
	box := cube.Box(
		math.Floor(center[0]-doubleRadius-1), math.Floor(center[1]-doubleRadius-1), math.Floor(center[2]-doubleRadius-1),
		math.Floor(center[0]+doubleRadius+1), math.Floor(center[1]+doubleRadius+1), math.Floor(center[2]+doubleRadius+1),
	)
	for e := range tx.EntitiesWithin(box) {
		if e.H() == source || spectator(e) || gustIgnores(e, blocklike) {
			continue
		}
		dist := e.Position().Sub(center).Len() / doubleRadius
		if dist > 1 {
			continue
		}
		origin := EyePosition(e)
		if _, tnt := e.(*TNT); tnt {
			origin = e.Position()
		}
		direction := javaNormalize(origin.Sub(center))
		multiplier := knockback
		if creativeFlying(e) {
			multiplier = 0
		}
		exposure := 0.0
		if multiplier != 0 {
			exposure = block.ExplosionSeenPercent(tx, center, e.H().Type().BBox(e).Translate(e.Position()))
		}
		power := (1 - dist) * exposure * float64(multiplier) * (1 - explosionKnockbackResistance(e))
		pushEntity(e, direction.Mul(power))
	}
}

// gustIgnores ports the overrides of Entity.ignoreExplosion for a gust.
func gustIgnores(e world.Entity, blocklike bool) bool {
	switch e.H().Type() {
	case ItemType:
		return !blocklike
	case ArmourStandType:
		return !blocklike
	}
	if w, ok := e.(*Warden); ok {
		return w.state().diggingOrEmerging()
	}
	return false
}

// creativeFlying reports if e is a player flying in creative mode, which
// explosions do not push.
func creativeFlying(e world.Entity) bool {
	p, ok := e.(interface {
		GameMode() world.GameMode
		Flying() bool
	})
	return ok && p.GameMode() == world.GameModeCreative && p.Flying()
}

// pushEntity ports Entity.push: the impulse is added to the velocity of e.
func pushEntity(e world.Entity, impulse mgl64.Vec3) {
	if math.IsNaN(impulse.Len()) || math.IsInf(impulse.Len(), 0) {
		return
	}
	if v, ok := e.(interface {
		Velocity() mgl64.Vec3
		SetVelocity(mgl64.Vec3)
	}); ok {
		v.SetVelocity(v.Velocity().Add(impulse))
	}
}

// explosionKnockbackResistance returns the EXPLOSION_KNOCKBACK_RESISTANCE of
// e: 0.15 for every level of Blast Protection on its armour.
func explosionKnockbackResistance(e world.Entity) float64 {
	resistance := 0.0
	for _, stack := range wornArmour(e) {
		if ench, ok := stack.Enchantment(enchantment.BlastProtection); ok {
			resistance += float64(float32(.15) * float32(ench.Level()))
		}
	}
	return min(1, max(0, resistance))
}

// wornArmour returns the armour pieces e wears.
func wornArmour(e world.Entity) []item.Stack {
	switch a := e.(type) {
	case interface{ Armour() *inventory.Armour }:
		return a.Armour().Items()
	case interface {
		EquipmentArmour() (item.Stack, item.Stack, item.Stack, item.Stack)
	}:
		head, chest, legs, feet := a.EquipmentArmour()
		return []item.Stack{head, chest, legs, feet}
	}
	return nil
}

// WindBurst ports the explosion of the Wind Burst enchantment of a mace after
// a smash attack: a gust at the attacker that launches it back up.
func WindBurst(tx *world.Tx, attacker world.Entity, level int) {
	if level <= 0 {
		return
	}
	knockback := [...]float32{1.2, 1.75, 2.2}
	multiplier := float32(1.5) + float32(.35)*float32(level-1)
	if level <= len(knockback) {
		multiplier = knockback[level-1]
	}
	windGust(tx, nil, attacker.Position(), 3.5, multiplier, true)
}

// MaceSmashKnockback ports MaceItem.knockback: the living entities around
// the one a mace smashed are thrown away from it.
func MaceSmashKnockback(tx *world.Tx, attacker, target world.Entity, fallDistance float64) {
	box := target.H().Type().BBox(target).Translate(target.Position()).Grow(3.5)
	for e := range tx.EntitiesWithin(box) {
		nearby, ok := e.(Living)
		if !ok || spectator(e) || e.H() == attacker.H() || e.H() == target.H() || creativeFlying(e) {
			continue
		}
		if tamedBy(e, attacker) {
			continue
		}
		direction := nearby.Position().Sub(target.Position())
		if direction.LenSqr() > 3.5*3.5 {
			continue
		}
		scale := 1.0
		if fallDistance > 5 {
			scale = 2
		}
		power := (3.5 - direction.Len()) * float64(float32(.7)) * scale * (1 - entityKnockbackResistance(e))
		if power <= 0 {
			continue
		}
		v := javaNormalize(direction).Mul(power)
		pushEntity(e, mgl64.Vec3{v[0], float64(float32(.7)), v[2]})
	}
}

// tamedBy reports if e is tamed by owner, as TamableAnimal.isOwnedBy.
func tamedBy(e, owner world.Entity) bool {
	switch t := e.(type) {
	case interface {
		Tamed() bool
		Owner() *world.EntityHandle
	}:
		return t.Tamed() && t.Owner() == owner.H()
	case interface {
		Tamed() bool
		OwnerUUID() string
	}:
		return t.Tamed() && t.OwnerUUID() == owner.H().UUID().String()
	}
	return false
}
