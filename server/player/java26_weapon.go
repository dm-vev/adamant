package player

import (
	"iter"
	"math"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

const kineticTicksPerSecond = 20

// tryKineticWeapon advances the kinetic weapon's charged stab while it is held.
// It resolves every eligible entity along the weapon's attack line, nearest first,
// stopping at the first colliding block.
func (p *Player) tryKineticWeapon(weapon item.KineticWeapon) {
	info := weapon.KineticWeaponInfo()
	ticksUsed := int(p.usingTicks)
	if ticksUsed < info.DelayTicks {
		return
	}
	ticksUsed -= info.DelayTicks
	look := p.Rotation().Vec3()
	attackerSpeed := look.Dot(p.Velocity().Mul(kineticTicksPerSecond))
	start := p.Position().Add(mgl64.Vec3{0, p.EyeHeight(), 0})
	maxReach := 4.5
	if p.GameMode().CreativeInventory() {
		maxReach = 6.5
	}
	end := start.Add(look.Mul(maxReach + 0.125))
	box := cube.Box(-0.125, -0.125, -0.125, 0.125, 0.125, 0.125)
	currentTick := p.tx.CurrentTick()
	for {
		hit, ok := trace.Perform(start, end, p.tx, box, func(entities iter.Seq[world.Entity]) iter.Seq[world.Entity] {
			return func(yield func(world.Entity) bool) {
				for candidate := range entities {
					if lastHit, wasHit := p.kineticRecent[candidate.H()]; wasHit && currentTick-lastHit < int64(info.ContactCooldownTicks) {
						continue
					}
					if !entity.CanBeHitByPiercingWeapon(candidate, p) {
						continue
					}
					if _, isLiving := candidate.(entity.Living); !isLiving {
						continue
					}
					if !yield(candidate) {
						return
					}
				}
			}
		}, trace.CollisionShape)
		if !ok {
			return
		}
		hitEntity, isEntity := hit.(trace.EntityResult)
		if !isEntity {
			return
		}
		target := hitEntity.Entity()
		if target == nil || start.Sub(hit.Position()).Len() < 1.875 {
			return
		}
		p.kineticRecent[target.H()] = currentTick
		targetLiving := target.(entity.Living)
		targetSpeed := look.Dot(targetLiving.Velocity().Mul(kineticTicksPerSecond))
		relativeSpeed := math.Max(0, attackerSpeed-targetSpeed)
		dealsDismount := info.Dismount.Matches(ticksUsed, attackerSpeed, relativeSpeed, 1)
		dealsKnockback := info.Knockback.Matches(ticksUsed, attackerSpeed, relativeSpeed, 1)
		dealsDamage := info.Damage.Matches(ticksUsed, attackerSpeed, relativeSpeed, 1)
		if !dealsDismount && !dealsKnockback && !dealsDamage {
			continue
		}

		force, height, critical := 0.4, 0.0, false
		ctx := NewEventContext(p.tx, p)
		if p.Handler().HandleAttackEntity(ctx, target, &force, &height, &critical); ctx.Cancelled() {
			continue
		}
		damage := 1 + math.Floor(relativeSpeed*info.DamageMultiplier)
		affected := false
		if dealsDamage {
			_, affected = targetLiving.Hurt(damage, world.DamageSource(entity.SpearDamageSource{AttackDamageSource: entity.AttackDamageSource{Attacker: p}}))
		}
		if dealsKnockback {
			targetLiving.KnockBack(p.Position(), force, height)
			velocity := p.Velocity()
			p.SetVelocity(mgl64.Vec3{velocity[0] * 0.6, velocity[1], velocity[2] * 0.6})
			affected = true
		}
		if dealsDismount {
			affected = entity.DismountRider(target, p.tx) || affected
		}
		if affected {
			p.SwingArm()
			if spear, ok := weapon.(item.Spear); ok {
				p.PlaySound(sound.Custom{Name: spearHitSound(spear), Volume: 1, Pitch: 1})
			}
		}
		useCtx := p.useContext()
		useCtx.DamageItem(1)
		p.handleUseContext(useCtx)
		p.usingStack, _ = p.HeldItems()
	}
}

// attackPiercingWeapon applies the Java piercing attack to every valid entity intersected by the stab ray.
func (p *Player) attackPiercingWeapon(info item.PiercingWeaponInfo) bool {
	p.SwingArm()
	held, _ := p.HeldItems()
	start := p.Position().Add(mgl64.Vec3{0, p.EyeHeight(), 0})
	maxReach := 4.5
	if p.GameMode().CreativeInventory() {
		maxReach = 6.5
	}
	end := start.Add(p.Rotation().Vec3().Mul(maxReach + 0.125))
	box := cube.Box(-0.125, -0.125, -0.125, 0.125, 0.125, 0.125)
	seen := make(map[*world.EntityHandle]struct{})
	hitSomething := false
	for {
		hit, ok := trace.Perform(start, end, p.tx, box, func(entities iter.Seq[world.Entity]) iter.Seq[world.Entity] {
			return func(yield func(world.Entity) bool) {
				for candidate := range entities {
					if _, wasHit := seen[candidate.H()]; wasHit || !entity.CanBeHitByPiercingWeapon(candidate, p) {
						continue
					}
					if _, isLiving := candidate.(entity.Living); !isLiving {
						continue
					}
					if !yield(candidate) {
						return
					}
				}
			}
		}, trace.CollisionShape)
		if !ok {
			break
		}
		hitEntity, isEntity := hit.(trace.EntityResult)
		if !isEntity {
			break
		}
		target := hitEntity.Entity()
		if target == nil || start.Sub(hit.Position()).Len() < 1.875 {
			break
		}
		seen[target.H()] = struct{}{}
		living := target.(entity.Living)
		damage := held.AttackDamage()
		if sharpness, ok := held.Enchantment(enchantment.Sharpness); ok {
			damage += enchantment.Sharpness.Addend(sharpness.Level())
		}
		if strength, ok := p.Effect(effect.Strength); ok {
			damage += damage * effect.Strength.Multiplier(strength.Level())
		}
		if weakness, ok := p.Effect(effect.Weakness); ok {
			damage -= damage * effect.Weakness.Multiplier(weakness.Level())
		}
		force, height, critical := 0.4, 0.0, false
		ctx := NewEventContext(p.tx, p)
		if p.Handler().HandleAttackEntity(ctx, target, &force, &height, &critical); ctx.Cancelled() {
			continue
		}
		_, affected := living.Hurt(damage, world.DamageSource(entity.SpearDamageSource{AttackDamageSource: entity.AttackDamageSource{Attacker: p}}))
		if info.DealsKnockback {
			if knockback, ok := held.Enchantment(enchantment.Knockback); ok {
				force += enchantment.Knockback.Force(knockback.Level())
			}
			living.KnockBack(p.Position(), force, height)
			velocity := p.Velocity()
			p.SetVelocity(mgl64.Vec3{velocity[0] * 0.6, velocity[1], velocity[2] * 0.6})
			affected = true
		}
		if info.Dismount {
			affected = entity.DismountRider(target, p.tx) || affected
		}
		hitSomething = hitSomething || affected
		current, left := p.HeldItems()
		if durable, ok := current.Item().(item.Durable); ok {
			p.SetHeldItems(p.damageItem(current, durable.DurabilityInfo().AttackDurability), left)
			held, _ = p.HeldItems()
		}
	}
	if spear, ok := held.Item().(item.Spear); ok {
		p.tx.PlaySound(p.Position(), sound.Custom{Name: spearAttackSound(spear), Volume: 1, Pitch: 1})
		if hitSomething {
			p.tx.PlaySound(p.Position(), sound.Custom{Name: spearHitSound(spear), Volume: 1, Pitch: 1})
		}
	}
	if lunge, ok := held.Enchantment(enchantment.Lunge); ok {
		p.applyLunge(lunge.Level())
	}
	p.Exhaust(0.1)
	return true
}

// applyLunge runs the Java POST_PIERCING_ATTACK effect for Lunge.
func (p *Player) applyLunge(level int) {
	if p.Riding() != nil || p.Gliding() || p.insideOfWater() || (!p.GameMode().CreativeInventory() && p.Food() < 7) {
		return
	}
	forward := p.Rotation().Vec3()
	p.SetVelocity(p.Velocity().Add(mgl64.Vec3{forward[0], 0, forward[2]}.Mul(enchantment.Lunge.Impulse(level))))
	p.Exhaust(float64(level) * 4)
	current, left := p.HeldItems()
	if _, ok := current.Item().(item.Durable); ok {
		p.SetHeldItems(p.damageItem(current, 1), left)
	}
	p.tx.PlaySound(p.Position(), sound.Custom{Name: "item.spear.lunge_" + string(rune('1'+rand.IntN(3))), Volume: 1, Pitch: 1})
}

func spearAttackSound(spear item.Spear) string {
	if spear.Tier == item.ToolTierWood {
		return "item.spear_wood.attack"
	}
	return "item.spear.attack"
}

func spearHitSound(spear item.Spear) string {
	if spear.Tier == item.ToolTierWood {
		return "item.spear_wood.hit"
	}
	return "item.spear.hit"
}
