package player

import (
	"math"
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// swordAttackSpeed is the attack speed of a player holding a sword: the
// base 4 of minecraft:attack_speed with the -2.4 of every sword
// (Item.Properties.sword).
const swordAttackSpeed = 1.6

// tickAttackStrength counts the ticks of Player.attackStrengthTicker as
// Player.tick does: one a tick, and none since the main hand came to hold
// another item (ItemStack.isSameItem), which restarts the count.
func (p *Player) tickAttackStrength() {
	p.attackStrengthTicks++
	held, _ := p.HeldItems()
	var name string
	var meta int16
	if !held.Empty() {
		name, meta = held.Item().EncodeItem()
	}
	if name != p.lastMainHand.name || meta != p.lastMainHand.meta {
		p.attackStrengthTicks = 0
		p.lastMainHand.name, p.lastMainHand.meta = name, meta
	}
}

// takeAttackStrength returns the attack strength of a player holding a
// sword, then restarts its count, as Player.attack reads
// getAttackStrengthScale(0.5) and then onAttack resets the ticker. The
// strength is the share of the delay of the attack speed counted, at most
// 1. A punch at the air restarts the count too (handlePunch).
func (p *Player) takeAttackStrength() float64 {
	speed := swordAttackSpeed
	// Haste and Mining Fatigue change minecraft:attack_speed by a tenth a
	// level (add_multiplied_total).
	if haste, ok := p.Effect(effect.Haste); ok {
		speed *= 1 + 0.1*float64(haste.Level())
	}
	if fatigue, ok := p.Effect(effect.MiningFatigue); ok {
		speed *= 1 - 0.1*float64(fatigue.Level())
	}
	strength := 1.0
	if speed > 0 {
		strength = min(max((float64(p.attackStrengthTicks)+0.5)/(20/speed), 0), 1)
	}
	p.attackStrengthTicks = 0
	return strength
}

// sweepAttack ports Player.isSweepAttack and doSweepAttack, after a hit of
// target that hurt it. A hit with a sword at full strength (more than 0.9),
// on the ground, neither critical nor sprinting, and slower horizontally
// than 2.5 times the movement speed of the player also hurts each living
// entity around the target: one whose box meets the target's grown by 1
// sideways and 0.25 up and down, within 3 blocks of the player. It takes 1
// plus the sweeping damage ratio times base, the damage of the hit before
// enchantments, scaled as the hit (baseDamageScaleFactor), then the
// enchantments of the sword for it, times the strength, and is knocked back
// 0.4 the way the player faces.
func (p *Player) sweepAttack(target world.Entity, held item.Stack, base, strength float64, critical bool) {
	if _, sword := held.Item().(item.Sword); !sword || strength <= 0.9 || critical || p.Sprinting() || !p.OnGround() {
		return
	}
	v, most := p.Velocity(), p.Speed()*2.5
	if v[0]*v[0]+v[2]*v[2] >= most*most {
		return
	}
	damage := 1 + p.sweepingDamageRatio(held)*base*(0.2+strength*strength*0.8)
	area := target.H().Type().BBox(target).Translate(target.Position()).GrowVec3(mgl64.Vec3{1, 0.25, 1})
	yaw := p.Rotation().Yaw() * math.Pi / 180
	facing := mgl64.Vec3{-math.Sin(yaw), 0, math.Cos(yaw)}
	src := world.DamageSource(entity.AttackDamageSource{Attacker: p})
	// Entities are found by their position: a box grown by 3 holds the
	// position of any entity whose box meets area.
	for e := range p.tx.EntitiesWithin(area.Grow(3)) {
		living, ok := e.(entity.Living)
		if !ok || e.H() == p.H() || e.H() == target.H() || living.Dead() || p.Position().Sub(e.Position()).LenSqr() >= 9 {
			continue
		}
		if !e.H().Type().BBox(e).Translate(e.Position()).IntersectsWith(area) {
			continue
		}
		dmg := (damage + p.enchantedDamageBonus(held, living)) * strength
		if _, hurt := living.Hurt(dmg, src); hurt && !living.H().Closed() {
			living.KnockBack(e.Position().Sub(facing), 0.4, 0.4)
			p.postAttackEffects(held, living)
		}
	}
}

// sweepingDamageRatio returns the minecraft:sweeping_damage_ratio of a
// player holding held: 0, the base of the attribute.
func (p *Player) sweepingDamageRatio(item.Stack) float64 {
	return 0
}

// enchantedDamageBonus returns the damage the enchantments of held add to
// a hit of living (Player.getEnchantedDamage): Sharpness, and Smite or Bane
// of Arthropods for an undead or arthropod target.
func (p *Player) enchantedDamageBonus(held item.Stack, living entity.Living) float64 {
	bonus := java26MobDamageBonus(held, living.H().Type().EncodeEntity())
	if s, ok := held.Enchantment(enchantment.Sharpness); ok {
		bonus += enchantment.Sharpness.Addend(s.Level())
	}
	return bonus
}

// postAttackEffects applies the effects held has on living after a hit
// that hurt it (EnchantmentHelper.doPostAttackEffects): Bane of
// Arthropods slows an arthropod, and Fire Aspect sets it on fire.
func (p *Player) postAttackEffects(held item.Stack, living entity.Living) {
	if _, ok := held.Enchantment(enchantment.BaneOfArthropods); ok && arthropodEntity(living.H().Type().EncodeEntity()) {
		if slowed, ok := living.(interface{ AddEffect(effect.Effect) }); ok {
			duration := time.Duration((1.5 + rand.Float64()*2) * float64(time.Second))
			slowed.AddEffect(effect.New(effect.Slowness, 4, duration))
		}
	}
	if f, ok := held.Enchantment(enchantment.FireAspect); ok {
		if flammable, ok := living.(entity.Flammable); ok {
			flammable.SetOnFire(enchantment.FireAspect.Duration(f.Level()))
		}
	}
}
