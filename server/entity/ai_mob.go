package entity

import (
	"math"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// MobAI is the AI of a Java Mob, ported from Mob and the movement of
// LivingEntity it drives: its goal and target selectors, its move, look,
// jump and body controls, its navigation and the physics it moves with. A
// species behaviour holds one and calls Tick from its Behaviour.Tick.
type MobAI struct {
	// persistent is Mob.persistenceRequired: the mob never leaves the world
	// of its own accord.
	persistent bool

	// Goals and Targets are the goal and target selectors of the mob.
	Goals, Targets GoalSelector

	// MovementSpeed, FollowRange, StepHeight, JumpStrength and Gravity are
	// the attributes of the mob its movement reads. Tick fills in Java's
	// defaults for those left zero.
	MovementSpeed, FollowRange, StepHeight, JumpStrength, Gravity float64
	// FlyingSpeed is the flying speed attribute a FlyingMoveControl flies
	// the mob with.
	FlyingSpeed float64
	// EyeHeight is the height of the eyes of the mob, 0.85 of its height if
	// zero.
	EyeHeight float64
	// MaxHeadYaw and HeadRotSpeed are Mob.getMaxHeadYRot and getHeadRotSpeed,
	// 75 and 10 if zero. MaxHeadPitch is getMaxHeadXRot, 40 if zero.
	MaxHeadYaw, HeadRotSpeed, MaxHeadPitch float64
	// Bounciness is the bounciness attribute, how much of its speed the mob
	// keeps bouncing off blocks it runs into. FrictionModifier and
	// AirDragModifier scale how much the ground and the air slow it, 1 if
	// zero.
	Bounciness, FrictionModifier, AirDragModifier float64
	// DiscardFriction keeps the air and the ground from slowing the mob, as
	// Entity.setDiscardFriction does for a long jump.
	DiscardFriction bool
	// NoAI stops the AI of the mob while it still moves.
	NoAI bool
	// NoGravity keeps the mob from falling, and NoPhysics moves it through
	// blocks, as Entity.isNoGravity and noPhysics.
	NoGravity, NoPhysics bool

	move moveControl
	// moveOverride, lookOverride and bodyOverride replace move, look and
	// body if set.
	moveOverride, lookOverride, bodyOverride mobMoveControl
	look                                     lookControl
	jump                                     jumpControl
	body                                     bodyRotationControl
	nav                                      PathNavigation
	sense                                    sensing

	maluses map[PathType]float64
	random  *vanilla.Random
	started bool

	tickCount    int
	noActionTime int
	home         mobHome
	aggressive   bool

	target          *world.EntityHandle
	lastHurtBy      *world.EntityHandle
	lastHurtByTime  int
	lastHurtMob     *world.EntityHandle
	lastHurtMobTime int
	// lastDamage is LivingEntity.lastDamageSource and lastDamageTime when
	// it was dealt, forgotten 40 ticks later.
	lastDamage     world.DamageSource
	lastDamageTime int

	// The damage cooldown of the LivingEntity: the world tick of the last
	// full hit and its damage.
	hurtCooldown bool
	hurtTick     int64
	lastHurt     float64

	// The movement state of the LivingEntity, with ridden set while a
	// player steers the mob.
	xxa, yya, zza, speed float64
	jumping, ridden      bool
	noJumpDelay          int
	headYaw, bodyYaw     float64
	prevPos              mgl64.Vec3
	onGround             bool
	collidedHorizontally bool
	collidedVertically   bool
	// collidedBelow is Entity.verticalCollisionBelow.
	collidedBelow bool
	waterHeight   float64
	lavaHeight    float64

	// useItem is the item the mob uses, as LivingEntity.useItem, and
	// useTicks how long it has used it.
	useItem  item.Stack
	useTicks int
	// knownSpeed is Entity.lastKnownSpeed, how far the mob moved in the
	// last tick.
	knownSpeed mgl64.Vec3
	// stabbed are the entities a kinetic weapon of the mob last hit, as
	// LivingEntity.recentKineticEnemies.
	stabbed map[*world.EntityHandle]int64
}

// Mob is a mob and its AI during a tick: what Java's goals and controls hold
// as their Mob.
type Mob struct {
	*Ent
	// AI is the AI of the mob.
	AI *MobAI
	// Self is the entity of the species, such as a *Zombie, which is passed
	// on where Java passes the mob itself.
	Self world.Entity
}

// Mob returns the mob with the AI passed as the entity self of e.
func (ai *MobAI) Mob(e *Ent, self world.Entity) *Mob {
	return &Mob{Ent: e, AI: ai, Self: self}
}

// start fills in the defaults of the AI the first time it runs.
func (ai *MobAI) start(m *Mob) {
	if ai.started {
		return
	}
	ai.started = true
	if ai.FollowRange == 0 {
		ai.FollowRange = 16
	}
	if ai.StepHeight == 0 {
		ai.StepHeight = .6
	}
	if ai.JumpStrength == 0 {
		ai.JumpStrength = .42
	}
	if ai.Gravity == 0 {
		ai.Gravity = .08
	}
	if ai.FrictionModifier == 0 {
		ai.FrictionModifier = 1
	}
	if ai.AirDragModifier == 0 {
		ai.AirDragModifier = 1
	}
	if ai.MaxHeadYaw == 0 {
		ai.MaxHeadYaw = 75
	}
	if ai.HeadRotSpeed == 0 {
		ai.HeadRotSpeed = 10
	}
	if ai.MaxHeadPitch == 0 {
		ai.MaxHeadPitch = 40
	}
	if ai.home == (mobHome{}) {
		ai.home = noHome
	}
	ai.headYaw, ai.bodyYaw = m.data.Rot.Yaw(), m.data.Rot.Yaw()
	ai.prevPos = m.data.Pos
	ai.onGround = m.data.OnGround
	ai.nav.init(m)
}

// Random returns the random source of the mob, Java's Mob.getRandom.
func (m *Mob) Random() *vanilla.Random {
	if m.AI.random == nil {
		r := vanilla.NewXoroshiroRandom(rand.Int64())
		m.AI.random = &r
	}
	return m.AI.random
}

// Tx returns the transaction the mob ticks in.
func (m *Mob) Tx() *world.Tx { return m.tx }

// Navigation returns the path navigation of the mob.
func (m *Mob) Navigation() *PathNavigation { return &m.AI.nav }

// MoveControl returns the move control of the mob.
func (m *Mob) MoveControl() *moveControl { return &m.AI.move }

// LookControl returns the look control of the mob.
func (m *Mob) LookControl() *lookControl { return &m.AI.look }

// JumpControl returns the jump control of the mob.
func (m *Mob) JumpControl() *jumpControl { return &m.AI.jump }

// TickCount returns the number of ticks the AI of the mob ran.
func (m *Mob) TickCount() int { return m.AI.tickCount }

// GameTime returns the time of the world in ticks.
func (m *Mob) GameTime() int64 { return m.tx.CurrentTick() }

// BBox returns the bounding box of the mob in the world.
func (m *Mob) BBox() cube.BBox {
	return m.H().Type().BBox(m.Self).Translate(m.data.Pos)
}

// Width returns the width of the bounding box of the mob.
func (m *Mob) Width() float64 { return m.H().Type().BBox(m.Self).Width() }

// Height returns the height of the bounding box of the mob.
func (m *Mob) Height() float64 { return m.H().Type().BBox(m.Self).Height() }

// EyeY returns the height of the eyes of the mob in the world.
func (m *Mob) EyeY() float64 {
	if m.AI.EyeHeight > 0 {
		return m.data.Pos[1] + m.AI.EyeHeight
	}
	return m.data.Pos[1] + m.Height()*.85
}

// BlockPos returns the position of the block the feet of the mob are in.
func (m *Mob) BlockPos() cube.Pos { return cube.PosFromVec3(m.data.Pos) }

// OnGround reports if the mob stands on something.
func (m *Mob) OnGround() bool { return m.AI.onGround }

// InWater reports if the mob touches water.
func (m *Mob) InWater() bool { return m.AI.waterHeight > 0 }

// InLava reports if the mob touches lava.
func (m *Mob) InLava() bool { return m.AI.lavaHeight > 0 }

// InLiquid reports if the mob touches water or lava.
func (m *Mob) InLiquid() bool { return m.InWater() || m.InLava() }

// FluidJumpThreshold returns how deep a mob may stand in a fluid before it
// swims up instead of jumping, as Entity.getFluidJumpThreshold.
func (m *Mob) FluidJumpThreshold() float64 {
	if f, ok := m.Behaviour().(interface{ fluidJumpThreshold(m *Mob) float64 }); ok {
		return f.fluidJumpThreshold(m)
	}
	if m.EyeY()-m.data.Pos[1] < .4 {
		return 0
	}
	return .4
}

// Speed returns the speed the mob walks with, LivingEntity.getSpeed.
func (m *Mob) Speed() float64 { return m.AI.speed }

// SetSpeed sets the speed of the mob and walks it forward with it, as
// Mob.setSpeed.
func (m *Mob) SetSpeed(speed float64) {
	m.AI.speed = speed
	m.AI.zza = speed
}

// SetZza, SetXxa and SetYya set the forward, sideways and upward input of
// the mob.
func (m *Mob) SetZza(v float64) { m.AI.zza = v }
func (m *Mob) SetXxa(v float64) { m.AI.xxa = v }
func (m *Mob) SetYya(v float64) { m.AI.yya = v }

// Yaw returns the yaw of the mob, Java's yRot.
func (m *Mob) Yaw() float64 { return m.data.Rot.Yaw() }

// SetYaw sets the yaw of the mob.
func (m *Mob) SetYaw(yaw float64) { m.data.Rot[0] = yaw }

// Pitch returns the pitch of the mob, Java's xRot.
func (m *Mob) Pitch() float64 { return m.data.Rot.Pitch() }

// SetPitch sets the pitch of the mob.
func (m *Mob) SetPitch(pitch float64) { m.data.Rot[1] = pitch }

// HeadYaw returns the yaw of the head of the mob.
func (m *Mob) HeadYaw() float64 { return m.AI.headYaw }

// StopInPlace stops the mob, as Mob.stopInPlace.
func (m *Mob) StopInPlace() {
	m.AI.nav.Stop()
	m.AI.xxa, m.AI.yya = 0, 0
	m.SetSpeed(0)
	m.data.Vel = mgl64.Vec3{}
}

// Alive reports if the mob is alive.
func (m *Mob) Alive() bool { return entityAlive(m.Self) }

// entityAlive reports if an entity is alive, as Entity.isAlive.
func entityAlive(e world.Entity) bool {
	if e == nil {
		return false
	}
	if l, ok := e.(Living); ok {
		return !l.Dead()
	}
	return !e.H().Closed()
}

// Aggressive reports if the mob is aggressive, Mob.isAggressive.
func (m *Mob) Aggressive() bool { return m.AI.aggressive }

// SetAggressive sets whether the mob is aggressive.
func (m *Mob) SetAggressive(v bool) { m.AI.aggressive = v }

// NoActionTime returns the ticks since the mob last had a player near or
// was hurt, LivingEntity.getNoActionTime.
func (m *Mob) NoActionTime() int { return m.AI.noActionTime }

// ResetNoActionTime resets the no action time of the mob.
func (m *Mob) ResetNoActionTime() { m.AI.noActionTime = 0 }

// Home returns the home restriction of the mob.
func (m *Mob) Home() mobHome { return m.AI.home }

// SetHome restricts the mob to radius blocks around pos, as Mob.setHomeTo.
func (m *Mob) SetHome(pos cube.Pos, radius int) { m.AI.home = mobHome{pos: pos, radius: radius} }

// ClearHome removes the home restriction of the mob.
func (m *Mob) ClearHome() { m.AI.home = noHome }

// WithinHome reports if a position is within the home of the mob, as
// Mob.isWithinHome.
func (m *Mob) WithinHome(pos cube.Pos) bool { return m.AI.home.contains(pos) }

// withinHomeVec ports Mob.isWithinHome(Vec3).
func (m *Mob) withinHomeVec(pos mgl64.Vec3) bool {
	h := m.AI.home
	if !h.set() {
		return true
	}
	return h.pos.Vec3Centre().Sub(pos).LenSqr() < float64(h.radius*h.radius)
}

// PathfindingMalus returns the cost the mob adds to a path through a block
// of a type, negative where it never walks, as Mob.getPathfindingMalus.
func (m *Mob) PathfindingMalus(t PathType) float64 {
	if malus, ok := m.AI.maluses[t]; ok {
		return malus
	}
	return t.Malus()
}

// SetPathfindingMalus overrides the malus of a path type for the mob.
func (ai *MobAI) SetPathfindingMalus(t PathType, malus float64) {
	if ai.maluses == nil {
		ai.maluses = make(map[PathType]float64, 4)
	}
	ai.maluses[t] = malus
}

// Target returns the entity the mob attacks, or nil, as Mob.getTarget.
func (m *Mob) Target() world.Entity {
	if m.AI.target == nil {
		return nil
	}
	t, ok := m.AI.target.Entity(m.tx)
	if !ok {
		return nil
	}
	return m.asValidTarget(t)
}

// mobTargetListener is implemented by behaviours that react to the target
// of the mob changing, as overrides of Mob.setTarget do. targetSet runs
// before the target changes.
type mobTargetListener interface {
	targetSet(m *Mob, target world.Entity)
}

// SetTarget sets the entity the mob attacks, nil to attack none.
func (m *Mob) SetTarget(t world.Entity) {
	t = m.asValidTarget(t)
	// The listener runs before the target changes, so it still sees the
	// target the mob had.
	if l, ok := m.Behaviour().(mobTargetListener); ok {
		l.targetSet(m, t)
	}
	if t == nil {
		m.AI.target = nil
	} else {
		m.AI.target = t.H()
	}
}

// asValidTarget ports Mob.asValidTarget.
func (m *Mob) asValidTarget(t world.Entity) world.Entity {
	if t == nil {
		return nil
	}
	if creativeOrSpectator(t) || !m.CanAttack(t) {
		return nil
	}
	return t
}

// creativeOrSpectator reports if an entity is a player in creative or
// spectator mode.
func creativeOrSpectator(e world.Entity) bool {
	if p, ok := e.(interface{ GameMode() world.GameMode }); ok {
		mode := p.GameMode()
		return mode == world.GameModeCreative || mode == world.GameModeSpectator
	}
	return false
}

func spectator(e world.Entity) bool {
	if p, ok := e.(interface{ GameMode() world.GameMode }); ok {
		return p.GameMode() == world.GameModeSpectator
	}
	return false
}

// mobAttackRestrictor is implemented by species whose mobs may not attack
// some entities, as overrides of Mob.canAttack.
type mobAttackRestrictor interface {
	canAttackEntity(m *Mob, target world.Entity) bool
}

// CanAttack reports if the mob may attack an entity, as Mob.canAttack: no
// ghast, no player in peaceful and nothing that can't be seen as an enemy.
func (m *Mob) CanAttack(t world.Entity) bool {
	if t == nil || t.H().Type().EncodeEntity() == "minecraft:ghast" || !canBeSeenAsEnemy(t) {
		return false
	}
	if _, player := t.(interface{ GameMode() world.GameMode }); player && m.tx.World().Difficulty() == world.DifficultyPeaceful {
		return false
	}
	if r, ok := m.Behaviour().(mobAttackRestrictor); ok {
		return r.canAttackEntity(m, t)
	}
	return true
}

// canBeSeenAsEnemy ports LivingEntity.canBeSeenAsEnemy: a living entity that
// is alive, no spectator and not invulnerable.
func canBeSeenAsEnemy(e world.Entity) bool {
	if _, ok := e.(Living); !ok || !entityAlive(e) || spectator(e) {
		return false
	}
	if p, ok := e.(interface{ PlayingDead() bool }); ok && p.PlayingDead() {
		// Axolotl.canBeSeenAsEnemy.
		return false
	}
	if p, ok := e.(interface{ GameMode() world.GameMode }); ok && !p.GameMode().AllowsTakingDamage() {
		return false
	}
	return true
}

// LastHurtBy returns the living entity that last hurt the mob within 100
// ticks and the tick count when it did, as getLastHurtByMob.
func (m *Mob) LastHurtBy() (world.Entity, int) {
	if m.AI.lastHurtBy == nil {
		return nil, m.AI.lastHurtByTime
	}
	e, ok := m.AI.lastHurtBy.Entity(m.tx)
	if !ok {
		return nil, m.AI.lastHurtByTime
	}
	return e, m.AI.lastHurtByTime
}

// SetLastHurtBy records the living entity that hurt the mob, as
// LivingEntity.setLastHurtByMob.
func (ai *MobAI) SetLastHurtBy(e world.Entity) {
	if e == nil {
		ai.lastHurtBy = nil
		return
	}
	ai.lastHurtBy, ai.lastHurtByTime = e.H(), ai.tickCount
}

// DamageCooldown ports the damage cooldown of LivingEntity.hurtServer: for
// 10 ticks after a hit a mob takes only the damage by which a stronger hit
// exceeds it. It returns the damage the mob takes, and false if it takes
// none.
func (ai *MobAI) DamageCooldown(tx *world.Tx, d float64) (float64, bool) {
	now := tx.CurrentTick()
	if ai.hurtCooldown && now-ai.hurtTick < 10 {
		if d <= ai.lastHurt {
			return 0, false
		}
		d, ai.lastHurt = d-ai.lastHurt, d
		return d, true
	}
	ai.hurtCooldown, ai.hurtTick, ai.lastHurt = true, now, d
	return d, true
}

// Hurt records the attacker of damage the mob took, as LivingEntity.hurt
// does for the goals that react to it.
func (ai *MobAI) Hurt(src world.DamageSource) {
	ai.noActionTime = 0
	ai.lastDamage, ai.lastDamageTime = src, ai.tickCount
	if attacker := damageAttacker(src); attacker != nil {
		if _, ok := attacker.(Living); ok {
			ai.SetLastHurtBy(attacker)
		}
	}
}

// HurtRecently reports if the mob was hurt in the last half second, as
// LivingEntity.wasHurtRecently.
func (m *Mob) HurtRecently() bool {
	return m.AI.lastDamage != nil && m.AI.tickCount-m.AI.lastDamageTime < 10
}

// DamageAttacker returns the entity responsible for damage, if any: the
// attacker or the owner of a projectile, as DamageSource.getEntity.
func DamageAttacker(src world.DamageSource) world.Entity { return damageAttacker(src) }

// recordLastHurtMob records that attacker hurt target, as
// LivingEntity.setLastHurtMob.
func recordLastHurtMob(attacker, target world.Entity) {
	if attacker == nil {
		return
	}
	if r, ok := attacker.(interface{ SetLastHurtMob(world.Entity) }); ok {
		r.SetLastHurtMob(target)
		return
	}
	if m, ok := mobOf(attacker); ok {
		m.AI.setLastHurtMob(target)
	}
}

// damageAttacker returns the entity responsible for damage, if any.
func damageAttacker(src world.DamageSource) world.Entity {
	if attacker := raidDamageAttacker(src); attacker != nil {
		return attacker
	}
	if a, ok := src.(interface{ Attacker() world.Entity }); ok {
		return a.Attacker()
	}
	return nil
}

// LastDamage returns the damage the mob last took within the last 40 ticks,
// as LivingEntity.getLastDamageSource.
func (m *Mob) LastDamage() world.DamageSource {
	if m.AI.lastDamage == nil || m.AI.tickCount-m.AI.lastDamageTime > 40 {
		return nil
	}
	return m.AI.lastDamage
}

// LastHurtMob returns the entity the mob last hurt.
func (m *Mob) LastHurtMob() (world.Entity, int) {
	if m.AI.lastHurtMob == nil {
		return nil, m.AI.lastHurtMobTime
	}
	e, ok := m.AI.lastHurtMob.Entity(m.tx)
	if !ok {
		return nil, m.AI.lastHurtMobTime
	}
	return e, m.AI.lastHurtMobTime
}

// setLastHurtMob records the entity the mob hurt.
func (ai *MobAI) setLastHurtMob(e world.Entity) {
	ai.lastHurtMob, ai.lastHurtMobTime = e.H(), ai.tickCount
}

// DistanceToSqr returns the squared distance between the mob and an entity.
func (m *Mob) DistanceToSqr(e world.Entity) float64 {
	return m.data.Pos.Sub(e.Position()).LenSqr()
}

// entityEyeY returns the height of the eyes of an entity in the world.
func entityEyeY(e world.Entity) float64 {
	if eyed, ok := e.(Eyed); ok {
		return e.Position()[1] + eyed.EyeHeight()
	}
	if m, ok := mobOf(e); ok {
		return m.EyeY()
	}
	return e.Position()[1] + e.H().Type().BBox(e).Height()*.85
}

// mobOf returns the mob of an entity with a MobAI.
func mobOf(e world.Entity) (*Mob, bool) {
	holder, ok := e.(interface{ mobAI() (*MobAI, *Ent) })
	if !ok {
		return nil, false
	}
	ai, ent := holder.mobAI()
	if ai == nil {
		return nil, false
	}
	return ai.Mob(ent, e), true
}

// LookAt turns the mob towards an entity by at most maxYaw and maxPitch
// degrees, as Mob.lookAt.
func (m *Mob) LookAt(e world.Entity, maxYaw, maxPitch float64) {
	d := e.Position().Sub(m.data.Pos)
	var dy float64
	if _, ok := e.(Living); ok {
		dy = entityEyeY(e) - m.EyeY()
	} else {
		bb := e.H().Type().BBox(e).Translate(e.Position())
		dy = (bb.Min()[1]+bb.Max()[1])/2 - m.EyeY()
	}
	sd := math.Sqrt(d[0]*d[0] + d[2]*d[2])
	yaw := math.Atan2(d[2], d[0])*180/math.Pi - 90
	pitch := -(math.Atan2(dy, sd) * 180 / math.Pi)
	m.SetPitch(rotlerp(m.Pitch(), pitch, maxPitch))
	m.SetYaw(rotlerp(m.Yaw(), yaw, maxYaw))
}

// rotlerp turns angle a towards b by at most max degrees.
func rotlerp(a, b, max float64) float64 {
	diff := wrapDegrees(b - a)
	return a + math.Max(-max, math.Min(max, diff))
}

// wrapDegrees wraps an angle to [-180, 180), as Mth.wrapDegrees.
func wrapDegrees(a float64) float64 {
	a = math.Mod(a, 360)
	if a >= 180 {
		a -= 360
	}
	if a < -180 {
		a += 360
	}
	return a
}

// degreesDifference returns the shortest turn from a to b, as
// Mth.degreesDifference.
func degreesDifference(a, b float64) float64 { return wrapDegrees(b - a) }

// rotateTowards turns from towards to by at most max degrees, as
// Control.rotateTowards.
func rotateTowards(from, to, max float64) float64 {
	return from + math.Max(-max, math.Min(max, degreesDifference(from, to)))
}

// rotateIfNecessary keeps angle within max degrees of target, as
// Mth.rotateIfNecessary.
func rotateIfNecessary(angle, target, max float64) float64 {
	diff := degreesDifference(angle, target)
	clamped := math.Max(-max, math.Min(max, diff))
	return target - clamped
}
