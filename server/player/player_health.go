package player

import (
	"math"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// combatState is the combat memory of the player.
type combatState struct {
	absorptionHealth float64
	// attackStrengthTicks counts the ticks since the player last attacked,
	// punched the air or came to hold another item in its main hand, as
	// Java's Player.attackStrengthTicker (tickAttackStrength), and
	// lastMainHand is that item, by name and meta.
	attackStrengthTicks int
	lastMainHand        struct {
		name string
		meta int16
	}
	lastDamage  float64
	immuneUntil time.Time
	// The combat memory of the LivingEntity: the living entity that last
	// hurt the player, the damage it last took and the living entity it
	// last hurt, with the game ticks they happened at.
	lastHurtBy, lastHurtMob         *world.EntityHandle
	lastHurtByTick, lastHurtMobTick int64
	lastDamageSource                world.DamageSource
	lastDamageTick                  int64
}

// Health returns the current health of the player. It will always be lower than Player.MaxHealth().
func (p *Player) Health() float64 {
	return p.health.Health()
}

// MaxHealth returns the maximum amount of health that a player may have. The MaxHealth will always be higher
// than Player.Health().
func (p *Player) MaxHealth() float64 {
	return p.health.MaxHealth()
}

// SetMaxHealth sets the maximum health of the player. If the current health of the player is higher than the
// new maximum health, the health is set to the new maximum.
// SetMaxHealth panics if the max health passed is 0 or lower.
func (p *Player) SetMaxHealth(health float64) {
	p.health.SetMaxHealth(health)
	p.client().SendHealth(p.Health(), p.MaxHealth(), p.absorptionHealth)
}

// addHealth adds health to the player's current health.
func (p *Player) addHealth(health float64) {
	p.health.AddHealth(health)
	p.client().SendHealth(p.Health(), p.MaxHealth(), p.absorptionHealth)
}

// Heal heals the entity for a given amount of health. The source passed
// represents the cause of the healing, for example entity.FoodHealingSource if
// the entity healed by having a full food bar. If the health added to the
// original health exceeds the entity's max health, Heal will not add the full
// amount. If the health passed is negative, Heal will not do anything.
// Heal returns the amount of health regenerated.
func (p *Player) Heal(health float64, source world.HealingSource) float64 {
	if p.Dead() || health < 0 || !p.GameMode().AllowsTakingDamage() {
		return 0
	}
	ctx := NewEventContext(p.tx, p)
	if p.Handler().HandleHeal(ctx, &health, source); ctx.Cancelled() {
		return 0
	}
	oldHealth := p.Health()
	p.addHealth(health)
	return p.Health() - oldHealth
}

// Hurt hurts the player for a given amount of damage. The source passed
// represents the cause of the damage, for example entity.AttackDamageSource if
// the player is attacked by another entity. If the final damage exceeds the
// health that the player currently has, the player is killed and will have to
// respawn.
// If the damage passed is negative, Hurt will not do anything. Hurt returns the
// final damage dealt to the Player and if the Player was vulnerable to this
// kind of damage.
func (p *Player) Hurt(dmg float64, src world.DamageSource) (float64, bool) {
	w := p.tx.World()
	if !w.GameRuleBool("fire_damage") && src.Fire() {
		return 0, false
	}
	if !w.GameRuleBool("drowning_damage") {
		if _, ok := src.(entity.DrowningDamageSource); ok {
			return 0, false
		}
	}
	// Player.isInvulnerableTo: the is_fall and is_freezing damage types.
	switch src.(type) {
	case entity.FallDamageSource, block.StalagmiteDamageSource:
		if !w.GameRuleBool("fall_damage") {
			return 0, false
		}
	case entity.FreezeDamageSource:
		if !w.GameRuleBool("freeze_damage") {
			return 0, false
		}
	}
	_, fireResistant := p.Effect(effect.FireResistance)
	if p.frostWalkerProtectsFrom(src) || (fireResistant && src.Fire()) || p.Dead() || !p.GameMode().AllowsTakingDamage() || dmg < 0 {
		return 0, false
	}
	totalDamage := p.FinalDamageFrom(dmg, src)
	if holding, using := p.Blocking(); holding && using && src.ReducedByArmour() {
		if p.tryShieldBlock(dmg, totalDamage, src) {
			return 0, false
		}
	}
	damageLeft := totalDamage

	immune := time.Now().Before(p.immuneUntil)
	if immune {
		if damageLeft -= p.lastDamage; damageLeft <= 0 {
			return 0, false
		}
	}

	immunity := time.Second / 2
	ctx := NewEventContext(p.tx, p)
	if p.Handler().HandleHurt(ctx, &damageLeft, immune, &immunity, src); ctx.Cancelled() {
		return 0, false
	}
	p.ReleaseShoulderParrots()
	p.setAttackImmunity(immunity, totalDamage)
	p.lastDamageSource, p.lastDamageTick = src, p.tx.CurrentTick()
	if attacker, ok := entity.DamageAttacker(src).(entity.Living); ok {
		p.lastHurtBy, p.lastHurtByTick = attacker.H(), p.tx.CurrentTick()
	}

	if a := p.Absorption(); a > 0 {
		remaining := a - damageLeft
		p.SetAbsorption(remaining)
		damageLeft = max(0, damageLeft-a)
		if _, exists := p.Effect(effect.Absorption); exists && remaining <= 0 {
			p.RemoveEffect(effect.Absorption)
		}
	}

	if p.Health()-damageLeft <= mgl64.Epsilon && !src.IgnoreTotem() {
		hand, offHand := p.HeldItems()
		if _, ok := offHand.Item().(item.Totem); ok {
			p.applyTotemEffects()
			p.SetHeldItems(hand, offHand.Grow(-1))
			return 0, false
		} else if _, ok := hand.Item().(item.Totem); ok {
			p.applyTotemEffects()
			p.SetHeldItems(hand.Grow(-1), offHand)
			return 0, false
		}
	}

	p.addHealth(-damageLeft)
	if damageLeft > 0 {
		p.emitGameEvent(world.GameEventEntityDamage, p.Position(), nil)
	}

	if src.ReducedByArmour() {
		p.Exhaust(0.1)
		p.Armour().Damage(dmg, src.Fire(), p.damageItem)

		var origin world.Entity
		if s, ok := src.(entity.AttackDamageSource); ok {
			origin = s.Attacker
		} else if s, ok := src.(entity.ProjectileDamageSource); ok {
			origin = s.Owner
		}
		if l, ok := origin.(entity.Living); ok {
			if thornsDmg := p.Armour().ThornsDamage(p.damageItem); thornsDmg > 0 {
				l.Hurt(thornsDmg, enchantment.ThornsDamageSource{Owner: p})
			}
		}
	}

	pos := p.Position()
	viewers, release := p.viewers()
	for _, viewer := range viewers {
		viewer.ViewEntityAction(p, entity.HurtAction{})
	}
	releaseBorrowedViewers(p.tx, viewers, release)
	if src.Fire() {
		p.tx.PlaySound(pos, sound.Burning{})
	} else if _, ok := src.(entity.DrowningDamageSource); ok {
		p.tx.PlaySound(pos, sound.Drowning{})
	}

	p.Wake()
	entity.TriggerHurtEffects(p.tx, p, p.Effects())

	if p.Dead() {
		p.kill(src)
	}
	return totalDamage, true
}

// LastHurtByMob returns the living entity that hurt the player in the last
// 100 ticks and the game tick it last did, as LivingEntity.getLastHurtByMob
// and getLastHurtByMobTimestamp.
func (p *Player) LastHurtByMob() (world.Entity, int64) {
	if p.lastHurtBy == nil || p.tx.CurrentTick()-p.lastHurtByTick > 100 {
		return nil, p.lastHurtByTick
	}
	e, ok := p.lastHurtBy.Entity(p.tx)
	if !ok {
		return nil, p.lastHurtByTick
	}
	return e, p.lastHurtByTick
}

// LastHurtMob returns the living entity the player last hurt and the game
// tick it did, as LivingEntity.getLastHurtMob and getLastHurtMobTimestamp.
func (p *Player) LastHurtMob() (world.Entity, int64) {
	if p.lastHurtMob == nil {
		return nil, p.lastHurtMobTick
	}
	e, ok := p.lastHurtMob.Entity(p.tx)
	if !ok {
		return nil, p.lastHurtMobTick
	}
	return e, p.lastHurtMobTick
}

// SetLastHurtMob records the entity the player hurt, as
// LivingEntity.setLastHurtMob.
func (p *Player) SetLastHurtMob(e world.Entity) {
	p.lastHurtMob, p.lastHurtMobTick = nil, p.tx.CurrentTick()
	if _, ok := e.(entity.Living); ok {
		p.lastHurtMob = e.H()
	}
}

// LastDamageSource returns the damage the player took last if it took it
// within the ticks passed, as LivingEntity.getLastDamageSource.
func (p *Player) LastDamageSource(timeout int64) world.DamageSource {
	if p.tx.CurrentTick()-p.lastDamageTick > timeout {
		return nil
	}
	return p.lastDamageSource
}

// applyTotemEffects is an unexported function that is used to handle totem effects.
func (p *Player) applyTotemEffects() {
	p.addHealth(2 - p.Health())

	for _, e := range p.Effects() {
		p.RemoveEffect(e.Type())
	}

	p.AddEffect(effect.New(effect.Regeneration, 2, time.Second*40))
	p.AddEffect(effect.New(effect.FireResistance, 1, time.Second*40))
	p.AddEffect(effect.New(effect.Absorption, 2, time.Second*5))

	p.tx.PlaySound(p.Position(), sound.Totem{})

	viewers, release := p.viewers()
	for _, viewer := range viewers {
		viewer.ViewEntityAction(p, entity.TotemUseAction{})
	}
	releaseBorrowedViewers(p.tx, viewers, release)
}

// FinalDamageFrom resolves the final damage received by the player if it is attacked by the source passed
// with the damage passed. FinalDamageFrom takes into account things such as the armour worn and the
// enchantments on the individual pieces.
// The damage returned will be at the least 0.
func (p *Player) FinalDamageFrom(dmg float64, src world.DamageSource) float64 {
	dmg = max(dmg, 0)

	dmg -= p.Armour().DamageReduction(dmg, src)
	if res, ok := p.Effect(effect.Resistance); ok {
		dmg *= effect.Resistance.Multiplier(src, res.Level())
	}
	return dmg
}

// Explode ...
func (p *Player) Explode(src world.ExplosionSource, impact float64) {
	p.Hurt(math.Floor((impact*impact+impact)*3.5*src.Size()*2+1), entity.ExplosionDamageSource{Source: src})
	impulse := entity.ExplosionImpulse(p, src, impact).Mul(1 - p.Armour().KnockBackResistance())
	velocity := p.Velocity().Add(impulse)
	p.data.Vel = velocity
	p.SetVelocity(velocity)
}

// Prick ...
func (p *Player) Prick(pos cube.Pos) {
	p.Hurt(0.5, block.DamageSource{Block: p.tx.Block(pos)})
}

// SetAbsorption sets the absorption health of a player. This extra health shows as golden hearts and do not
// actually increase the maximum health. Once the hearts are lost, they will not regenerate.
// Nothing happens if a negative number is passed.
func (p *Player) SetAbsorption(health float64) {
	p.absorptionHealth = max(health, 0)
	p.client().SendHealth(p.Health(), p.MaxHealth(), p.absorptionHealth)
}

// Absorption returns the absorption health that the player has.
func (p *Player) Absorption() float64 {
	return p.absorptionHealth
}

// KnockBack knocks the player back with a given force and height. A source is passed which indicates the
// source of the velocity, typically the position of an attacking entity. The source is used to calculate the
// direction which the entity should be knocked back in.
func (p *Player) KnockBack(src mgl64.Vec3, force, height float64) {
	if p.Dead() || !p.GameMode().AllowsTakingDamage() {
		return
	}
	p.knockBack(src, force, height)
}

// knockBack is an unexported function that is used to knock the player back. This function does not check if the player
// can take damage or not.
func (p *Player) knockBack(src mgl64.Vec3, force, height float64) {
	velocity := p.Position().Sub(src)
	velocity[1] = 0

	if velocity.Len() != 0 {
		velocity = velocity.Normalize().Mul(force)
	}
	velocity[1] = height

	p.SetVelocity(velocity.Mul(1 - p.Armour().KnockBackResistance()))
}

// setAttackImmunity sets the duration the player is immune to entity attacks.
func (p *Player) setAttackImmunity(d time.Duration, dmg float64) {
	p.immuneUntil = time.Now().Add(d)
	p.lastDamage = dmg
}

// Blocking returns true if the player is currently blocking with a shield. The first boolean is true if the player was
// holding a shield. The second boolean is true if the player was performing the necessary actions in order to block.
func (p *Player) Blocking() (holding bool, using bool) {
	held, other := p.HeldItems()
	_, heldShield := held.Item().(item.Shield)
	_, otherShield := other.Item().(item.Shield)
	if p.BlockingDelay() > 0 || !p.sneaking || p.usingItem {
		return heldShield || otherShield, false
	}
	return heldShield || otherShield, true
}

// BlockingDelay returns the remaining duration of the player's shield blocking delay. If this value is larger than 0,
// they should not be able to use their shield.
func (p *Player) BlockingDelay() time.Duration {
	return time.Duration(p.blockingDelayTicks) * time.Second / 20
}

// SetBlockingDelay updates the current blocking delay to the new duration provided. If the player is currently blocking
// with a shield, they will no longer be blocking and cannot do so until the duration is over.
func (p *Player) SetBlockingDelay(duration time.Duration) {
	blocking, _ := p.Blocking()
	p.blockingDelayTicks = int64(duration.Seconds() * 20)
	if blocking {
		p.updateState()
	}
}

// AttackEntity uses the item held in the main hand of the player to attack the entity passed, provided it is
// within range of the player.
// The damage dealt to the entity will depend on the item held by the player and any effects the player may
// have.
// If the player cannot reach the target or a dragon part along the look ray, the method returns immediately.
//
// It follows Player.attack: cannotAttack, then the event and the swing, the damage the item and the effects
// make of the hit, the hurt, and what the hit leaves behind (the item, the sound, the sweep, the effects shown
// and the knock-back).
func (p *Player) AttackEntity(e world.Entity) bool {
	ac, end := p.beginAction()
	defer end()
	held, _ := p.HeldItems()
	_, piercing := held.Item().(item.PiercingWeapon)
	if p.cannotAttack(ac, e, piercing) {
		return false
	}
	living, isLiving := e.(entity.Living)

	var (
		force, height = 0.45, 0.3608
		critical      = p.canCriticalAttack()
	)

	if piercing, ok := held.Item().(item.PiercingWeapon); ok {
		return p.attackPiercingWeapon(piercing.PiercingWeaponInfo())
	}
	if k, ok := held.Enchantment(enchantment.Knockback); ok {
		inc := enchantment.Knockback.Force(k.Level())
		force += inc
		height += inc
	}

	destructible, isDestructible := e.(entity.Destructible)
	attackable, isAttackable := e.(entity.Attackable)

	ctx := NewEventContext(p.tx, p)
	if p.Handler().HandleAttackEntity(ctx, e, &force, &height, &critical); ctx.Cancelled() {
		return false
	}
	if !isLiving && !isDestructible && !isAttackable {
		p.SwingArm()
		return false
	}

	p.SwingArm()
	strength := p.takeAttackStrength()

	i, left := p.HeldItems()
	if isAttackable {
		attackable.Attack(p, p.tx)
	}
	if !isLiving {
		return p.attackNonLiving(i, left, destructible, isAttackable)
	}

	smash := p.maceSmash(i)
	dmg, base := p.attackDamage(i, living, smash, critical)
	n, vulnerable := living.Hurt(dmg, p.createAttackSource(smash))
	if vulnerable {
		p.SetLastHurtMob(e)
	}
	i, left = p.HeldItems()

	p.itemHurtEnemy(i, left)

	p.tx.PlaySound(entity.EyePosition(e), sound.Attack{Damage: !mgl64.FloatEqual(n, 0)})
	if smash.ready && vulnerable {
		p.maceHurtEnemy(e, smash)
	}
	if !vulnerable {
		return true
	}
	p.sweepAttack(e, i, base, strength, critical)
	p.Exhaust(0.1)
	if living.H().Closed() {
		// Hurt may synchronously kill and remove the mob. Its viewers no
		// longer have a runtime ID for subsequent hit effects or metadata.
		return true
	}
	p.attackVisualEffects(living, critical)

	living.KnockBack(p.Position(), force, height)
	p.postAttackEffects(i, living)
	return true
}

// cannotAttack returns true if the player may not attack e (Player.cannotAttack): the target, or a dragon part
// along the look ray, is out of reach, it is a player while pvp is off, it is dead, or the anti-cheat refuses the
// attack. A piercing weapon does not need the target to be in reach.
func (p *Player) cannotAttack(ac *acState, e world.Entity, piercing bool) bool {
	reach := 8.0
	if p.GameMode().CreativeInventory() || piercing {
		reach = 14
	}
	if target, ok := e.(interface {
		PartHitFrom(mgl64.Vec3, mgl64.Vec3, float64) (string, mgl64.Vec3, bool)
	}); ok {
		_, hitPos, hit := target.PartHitFrom(entity.EyePosition(p), p.Rotation().Vec3(), reach)
		if !hit || (!piercing && !p.canReachEntity(ac, e, &hitPos)) {
			return true
		}
	} else if !piercing && !p.canReachEntity(ac, e, nil) {
		return true
	}
	if _, isPlayer := e.(*Player); isPlayer && !p.tx.World().GameRuleBool("pvp") {
		return true
	}

	if living, isLiving := e.(entity.Living); isLiving && living.Dead() {
		return true
	}
	return p.refuseAttack(ac, e)
}

// canCriticalAttack returns true if a hit of the player is critical (Player.canCriticalAttack): it is falling,
// neither sprinting nor flying, and has neither Slow Falling nor Blindness.
func (p *Player) canCriticalAttack() bool {
	_, slowFalling := p.Effect(effect.SlowFalling)
	_, blind := p.Effect(effect.Blindness)
	return !p.Sprinting() && !p.Flying() && p.FallDistance() > 0 && !slowFalling && !blind
}

// attackNonLiving finishes an attack of a target that is not a living entity, after it was told of it: a
// target that can only be attacked is done with, and a destructible one is destroyed. The item i of the main
// hand, with the one in the off hand, takes the damage of an attack when the target took the hit.
func (p *Player) attackNonLiving(i, left item.Stack, destructible entity.Destructible, isAttackable bool) bool {
	if isAttackable && destructible == nil {
		p.itemHurtEnemy(i, left)
		p.Exhaust(0.1)
		return true
	}

	src := world.DamageSource(entity.AttackDamageSource{Attacker: p})
	if !destructible.Destroy(p.tx, src, p) {
		return false
	}
	p.itemHurtEnemy(i, left)
	p.Exhaust(0.1)
	return true
}

// itemHurtEnemy damages the item i of the main hand by what an attack costs it (ItemStack.hurtEnemy), if it
// has a durability. left is the item of the off hand.
func (p *Player) itemHurtEnemy(i, left item.Stack) {
	if durable, ok := i.Item().(item.Durable); ok {
		p.SetHeldItems(p.damageItem(i, durable.DurabilityInfo().AttackDurability), left)
	}
}

// maceSmash is what the fall of the player makes of a hit with a mace (MaceItem.canSmashAttack and
// getAttackDamageBonus).
type maceSmash struct {
	// ready is true if the hit smashes: the player holds a mace, has fallen far enough, is not gliding, and
	// the fall adds damage.
	ready bool
	// bonus is the damage the smash adds, with Density.
	bonus float64
	// fallDistance is how far the player has fallen.
	fallDistance float64
	// breachLevel and windLevel are the levels of Breach and Wind Burst on the mace.
	breachLevel, windLevel int
}

// maceSmash returns the smash of a hit with the item i of the main hand.
func (p *Player) maceSmash(i item.Stack) (s maceSmash) {
	s.fallDistance = p.FallDistance()
	mace, ok := i.Item().(item.Mace)
	if !ok {
		return s
	}
	if b, ok := i.Enchantment(enchantment.Breach); ok {
		s.breachLevel = b.Level()
	}
	if w, ok := i.Enchantment(enchantment.WindBurst); ok {
		s.windLevel = w.Level()
	}
	effective := mace.EffectiveFallDistance(s.fallDistance)
	// MaceItem.canSmashAttack: falling more than 1.5 blocks, not gliding.
	if effective > mace.SmashThreshold() && !p.Gliding() {
		smashDistance := effective
		s.bonus = mace.SmashBonus(smashDistance)
		if dens, ok := i.Enchantment(enchantment.Density); ok {
			s.bonus += enchantment.Density.AdditionalDamage(dens.Level(), smashDistance)
		}
		if s.bonus > 0 {
			s.ready = true
		}
	}
	return s
}

// attackDamage returns the damage of a hit of living with the item i of the main hand (Player.attack):
// the damage of the item, the smash, Strength and Weakness, the enchantments and the bonuses against the
// mob, and a critical hit's half more. base is the damage before the enchantments, as the sweep scales it.
func (p *Player) attackDamage(i item.Stack, living entity.Living, smash maceSmash, critical bool) (dmg, base float64) {
	dmg = i.AttackDamage()
	if smash.ready {
		dmg += smash.bonus
	}
	if strength, ok := p.Effect(effect.Strength); ok {
		dmg += dmg * effect.Strength.Multiplier(strength.Level())
	}
	if weakness, ok := p.Effect(effect.Weakness); ok {
		dmg -= dmg * effect.Weakness.Multiplier(weakness.Level())
	}
	base = dmg
	if s, ok := i.Enchantment(enchantment.Sharpness); ok {
		dmg += enchantment.Sharpness.Addend(s.Level())
		p.magicCrit(living)
	}
	target := living.H().Type().EncodeEntity()
	dmg += java26MobDamageBonus(i, target)
	if imp, ok := i.Enchantment(enchantment.Impaling); ok {
		if bonus := enchantment.Impaling.Damage(imp.Level(), living, p.tx); bonus > 0 {
			dmg += bonus
		}
	}
	if critical {
		dmg *= 1.5
	}
	return dmg, base
}

// createAttackSource returns the damage source of a hit with the smash passed (ItemStack.getDamageSource): a
// smash with Breach ignores part of the armour.
func (p *Player) createAttackSource(smash maceSmash) world.DamageSource {
	src := world.DamageSource(entity.AttackDamageSource{Attacker: p})
	if smash.ready && smash.breachLevel > 0 {
		if armourMultiplier := enchantment.Breach.ArmourMultiplier(smash.breachLevel); armourMultiplier != 1 {
			src = world.DamageSource(entity.MaceSmashDamageSource{AttackDamageSource: entity.AttackDamageSource{Attacker: p}, ArmourMultiplier: armourMultiplier})
		}
	}
	return src
}

// maceHurtEnemy applies a smash that hurt e (MaceItem.hurtEnemy): the attacker stops falling and the entities
// around its target are thrown away, then Wind Burst launches the attacker back up before
// MaceItem.postHurtEnemy resets its fall.
func (p *Player) maceHurtEnemy(e world.Entity, smash maceSmash) {
	velocity := p.Velocity()
	velocity[1] = float64(float32(.01))
	p.SetVelocity(velocity)
	entity.MaceSmashKnockback(p.tx, p, e, smash.fallDistance)
	if smash.windLevel > 0 {
		entity.WindBurst(p.tx, p, smash.windLevel)
	}
	p.ResetFallDistance()
}

// magicCrit shows the enchanted hit on living (Player.magicCrit).
func (p *Player) magicCrit(living entity.Living) {
	viewers, release := borrowViewers(p.tx, living.Position())
	// Enchanted hit particles trigger frequently; reuse the pooled buffer so multiple players swinging simultaneously doesn't allocate.
	for _, v := range viewers {
		v.ViewEntityAction(living, entity.EnchantedHitAction{})
	}
	releaseBorrowedViewers(p.tx, viewers, release)
}

// attackVisualEffects shows the others what a hit of living leaves to see (Player.attackVisualEffects): a
// critical hit's stars.
func (p *Player) attackVisualEffects(living entity.Living, critical bool) {
	if critical {
		viewers, release := borrowViewers(p.tx, living.Position())
		// Critical hit notifications follow the same pooling strategy to avoid churning slices during sustained
		// fights.
		for _, v := range viewers {
			v.ViewEntityAction(living, entity.CriticalHitAction{})
		}
		releaseBorrowedViewers(p.tx, viewers, release)
	}
}

// SwingArm makes the player swing its arm.
func (p *Player) SwingArm() {
	if p.Dead() {
		return
	}
	duration := time.Millisecond * 300
	if e, ok := p.Effect(effect.Haste); ok {
		duration -= time.Duration(e.Level()) * time.Millisecond * 50
	} else if e, ok = p.Effect(effect.MiningFatigue); ok {
		duration += time.Duration(e.Level()*2) * time.Millisecond * 50
	}
	if duration > 0 {
		p.SetBlockingDelay(duration)
	}
	viewers, release := p.viewers()
	for _, v := range viewers {
		v.ViewEntityAction(p, entity.SwingArmAction{})
	}
	releaseBorrowedViewers(p.tx, viewers, release)
}

// PunchAir makes the player punch the air and plays the sound for attacking with no damage.
func (p *Player) PunchAir() {
	if p.Dead() {
		return
	}
	ctx := NewEventContext(p.tx, p)
	if p.Handler().HandlePunchAir(ctx); ctx.Cancelled() {
		return
	}
	p.SwingArm()
	// ServerGamePacketListenerImpl.handlePunch restarts the attack strength.
	p.attackStrengthTicks = 0
	p.tx.PlaySound(p.Position(), sound.Attack{})
}

// tryShieldBlock tries to block an attack with a shield. If the attack was blocked, true is returned. If the
// attack was not blocked, false is returned.
func (p *Player) tryShieldBlock(dmg float64, totalDamage float64, src world.DamageSource) bool {
	holding, using := p.Blocking()
	if !holding || !using || !src.ReducedByArmour() {
		return false
	}

	if attack, ok := src.(entity.AttackDamageSource); ok {
		if attack.Attacker != nil {
			diff := p.Position().Sub(attack.Attacker.Position())
			diff[1] = 0
			if diff.Dot(p.Rotation().Vec3()) >= 0 {
				return false
			}
		}
	}

	p.tx.PlaySound(p.Position(), sound.ShieldBlock{})
	p.SetBlockingDelay(time.Millisecond * 250)

	if attack, ok := src.(entity.AttackDamageSource); ok {
		if attacker, ok := attack.Attacker.(entity.Living); ok {
			attacker.KnockBack(p.Position(), 0.5, 0.4)
		}
		if other, ok := attack.Attacker.(*Player); ok {
			held, _ := other.HeldItems()
			if _, axe := held.Item().(item.Axe); axe {
				p.SetBlockingDelay(time.Second * 5)
			}
		}
	}

	if dmg >= 3 {
		damage := int(math.Ceil(totalDamage))
		held, other := p.HeldItems()
		if _, ok := held.Item().(item.Shield); ok {
			p.SetHeldItems(p.damageItem(held, damage), other)
		} else {
			p.SetHeldItems(held, p.damageItem(other, damage))
		}
	}
	return true
}
