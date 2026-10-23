package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// FloatGoal makes a mob swim up in water and lava, ported from Java's
// FloatGoal.
type FloatGoal struct{ goalFlags }

// NewFloatGoal returns a FloatGoal and lets the navigation of the mob path
// across water.
func NewFloatGoal(m *Mob) *FloatGoal {
	m.AI.nav.SetCanFloat(true)
	return &FloatGoal{goalFlags{GoalJump}}
}

func (g *FloatGoal) CanUse(m *Mob) bool {
	return m.AI.waterHeight > m.FluidJumpThreshold() || m.InLava()
}

func (g *FloatGoal) RequiresUpdateEveryTick() bool { return true }

func (g *FloatGoal) Tick(m *Mob) {
	if m.Random().NextFloat() < .8 {
		m.AI.jump.Jump()
	}
}

// RandomStrollGoal walks a mob to random positions now and then, ported from
// Java's RandomStrollGoal. With AvoidWater set, it is a
// WaterAvoidingRandomStrollGoal.
type RandomStrollGoal struct {
	goalFlags
	SpeedModifier     float64
	Interval          int
	CheckNoActionTime bool
	// AvoidWater prefers positions on land, leaving water with a chance of
	// Probability to walk anywhere, as WaterAvoidingRandomStrollGoal.
	AvoidWater  bool
	Probability float64
	// Position overrides where the goal walks to.
	Position func(m *Mob) (mgl64.Vec3, bool)

	wanted       mgl64.Vec3
	forceTrigger bool
}

// NewRandomStrollGoal returns a RandomStrollGoal with the interval of 120
// ticks of Java.
func NewRandomStrollGoal(speedModifier float64) *RandomStrollGoal {
	return &RandomStrollGoal{goalFlags: goalFlags{GoalMove}, SpeedModifier: speedModifier, Interval: 120, CheckNoActionTime: true}
}

// NewWaterAvoidingRandomStrollGoal returns a WaterAvoidingRandomStrollGoal.
func NewWaterAvoidingRandomStrollGoal(speedModifier float64) *RandomStrollGoal {
	g := NewRandomStrollGoal(speedModifier)
	g.AvoidWater, g.Probability = true, .001
	return g
}

func (g *RandomStrollGoal) CanUse(m *Mob) bool {
	if !g.forceTrigger {
		if g.CheckNoActionTime && m.NoActionTime() >= 100 {
			return false
		}
		if m.Random().NextInt(int32(reducedTickDelay(g.Interval))) != 0 {
			return false
		}
	}
	pos, ok := g.position(m)
	if !ok {
		return false
	}
	g.wanted, g.forceTrigger = pos, false
	return true
}

func (g *RandomStrollGoal) position(m *Mob) (mgl64.Vec3, bool) {
	if g.Position != nil {
		return g.Position(m)
	}
	if !g.AvoidWater {
		return DefaultRandomPos(m, 10, 7)
	}
	if m.InWater() {
		if pos, ok := LandRandomPos(m, 15, 7); ok {
			return pos, true
		}
		return DefaultRandomPos(m, 10, 7)
	}
	if float64(m.Random().NextFloat()) >= g.Probability {
		return LandRandomPos(m, 10, 7)
	}
	return DefaultRandomPos(m, 10, 7)
}

func (g *RandomStrollGoal) CanContinueToUse(m *Mob) bool { return !m.AI.nav.IsDone() }

func (g *RandomStrollGoal) Start(m *Mob) {
	m.AI.nav.MoveToPos(m, g.wanted[0], g.wanted[1], g.wanted[2], g.SpeedModifier)
}

func (g *RandomStrollGoal) Stop(m *Mob) { m.AI.nav.Stop() }

// Trigger makes the goal walk the next time it may.
func (g *RandomStrollGoal) Trigger() { g.forceTrigger = true }

// LookAtPlayerGoal turns the head of a mob towards a nearby player, or an
// entity of another type, now and then, ported from Java's
// LookAtPlayerGoal.
type LookAtPlayerGoal struct {
	goalFlags
	// IsType selects the entities looked at, players if nil.
	IsType         func(world.Entity) bool
	LookDistance   float64
	Probability    float64
	OnlyHorizontal bool

	lookAt   *world.EntityHandle
	lookTime int
}

// NewLookAtPlayerGoal returns a LookAtPlayerGoal at players within a
// distance with Java's probability of 0.02.
func NewLookAtPlayerGoal(distance float64) *LookAtPlayerGoal {
	return &LookAtPlayerGoal{goalFlags: goalFlags{GoalLook}, LookDistance: distance, Probability: .02}
}

func (g *LookAtPlayerGoal) conditions() targeting {
	c := forNonCombat()
	c.rangeLimit = g.LookDistance
	if g.IsType == nil {
		c.selector = func(m *Mob, target world.Entity) bool {
			return m.data.Riding != target.H() && !ridingOn(m.tx, target, m.H())
		}
	}
	return c
}

// ridingOn reports if e rides the entity with handle h.
func ridingOn(tx *world.Tx, e world.Entity, h *world.EntityHandle) bool {
	rider, ok := e.(Rider)
	return ok && rider.Riding() == h
}

func (g *LookAtPlayerGoal) CanUse(m *Mob) bool {
	if float64(m.Random().NextFloat()) >= g.Probability {
		return false
	}
	var target world.Entity
	if g.IsType == nil {
		target = nearestPlayer(m, g.conditions())
	} else {
		box := m.BBox().GrowVec3(mgl64.Vec3{g.LookDistance, 3, g.LookDistance})
		target = nearestOfTypes(m, g.conditions(), box, g.IsType)
	}
	if target == nil {
		g.lookAt = nil
		return false
	}
	g.lookAt = target.H()
	return true
}

func (g *LookAtPlayerGoal) target(m *Mob) (world.Entity, bool) {
	if g.lookAt == nil {
		return nil, false
	}
	return g.lookAt.Entity(m.tx)
}

func (g *LookAtPlayerGoal) CanContinueToUse(m *Mob) bool {
	t, ok := g.target(m)
	if !ok || !entityAlive(t) {
		return false
	}
	return m.DistanceToSqr(t) <= g.LookDistance*g.LookDistance && g.lookTime > 0
}

func (g *LookAtPlayerGoal) Start(m *Mob) {
	g.lookTime = adjustedTickDelay(g, 40+int(m.Random().NextInt(40)))
}

func (g *LookAtPlayerGoal) Stop(*Mob) { g.lookAt = nil }

func (g *LookAtPlayerGoal) Tick(m *Mob) {
	t, ok := g.target(m)
	if !ok || !entityAlive(t) {
		return
	}
	y := entityEyeY(t)
	if g.OnlyHorizontal {
		y = m.EyeY()
	}
	m.AI.look.SetLookAt(m, t.Position()[0], y, t.Position()[2])
	g.lookTime--
}

// RandomLookAroundGoal turns the head of an idle mob in random directions,
// ported from Java's RandomLookAroundGoal.
type RandomLookAroundGoal struct {
	goalFlags
	relX, relZ float64
	lookTime   int
}

// NewRandomLookAroundGoal returns a RandomLookAroundGoal.
func NewRandomLookAroundGoal() *RandomLookAroundGoal {
	return &RandomLookAroundGoal{goalFlags: goalFlags{GoalMove | GoalLook}}
}

func (g *RandomLookAroundGoal) CanUse(m *Mob) bool { return m.Random().NextFloat() < .02 }

func (g *RandomLookAroundGoal) CanContinueToUse(*Mob) bool { return g.lookTime >= 0 }

func (g *RandomLookAroundGoal) Start(m *Mob) {
	angle := math.Pi * 2 * m.Random().NextDouble()
	g.relX, g.relZ = math.Cos(angle), math.Sin(angle)
	g.lookTime = 20 + int(m.Random().NextInt(20))
}

func (g *RandomLookAroundGoal) RequiresUpdateEveryTick() bool { return true }

func (g *RandomLookAroundGoal) Tick(m *Mob) {
	g.lookTime--
	m.AI.look.SetLookAt(m, m.data.Pos[0]+g.relX, m.EyeY(), m.data.Pos[2]+g.relZ)
}

// mobMeleeAttacker is implemented by behaviours of mobs that hit their
// targets in melee, as Mob.doHurtTarget.
type mobMeleeAttacker interface {
	doHurtTarget(m *Mob, target world.Entity) bool
}

// doHurtTarget hits a target in melee and records it as the last mob hurt.
func (m *Mob) doHurtTarget(target world.Entity) bool {
	a, ok := m.Behaviour().(mobMeleeAttacker)
	if !ok {
		return false
	}
	if !a.doHurtTarget(m, target) {
		return false
	}
	m.AI.setLastHurtMob(target)
	return true
}

// defaultAttackReach is Mob.DEFAULT_ATTACK_REACH, the reach of a mob without
// a weapon that reaches further.
var defaultAttackReach = math.Sqrt(2.04) - .6

// WithinMeleeAttackRange reports if a mob reaches a target with a melee
// attack, as Mob.isWithinMeleeAttackRange: its box grown horizontally by
// its reach intersects that of the target.
func (m *Mob) WithinMeleeAttackRange(target world.Entity) bool {
	reach := defaultAttackReach
	box := m.BBox()
	if vehicle := m.data.Riding; vehicle != nil {
		if v, ok := vehicle.Entity(m.tx); ok {
			mount := v.H().Type().BBox(v).Translate(v.Position())
			box = cube.Box(math.Min(box.Min()[0], mount.Min()[0]), box.Min()[1], math.Min(box.Min()[2], mount.Min()[2]),
				math.Max(box.Max()[0], mount.Max()[0]), box.Max()[1], math.Max(box.Max()[2], mount.Max()[2]))
		}
	}
	box = box.GrowVec3(mgl64.Vec3{reach, 0, reach})
	hitbox := target.H().Type().BBox(target).Translate(target.Position())
	return box.IntersectsWith(hitbox)
}

// MeleeAttackGoal chases the target of a mob and hits it when in reach,
// ported from Java's MeleeAttackGoal.
type MeleeAttackGoal struct {
	goalFlags
	SpeedModifier                float64
	FollowingTargetEvenIfNotSeen bool
	// OnTick runs after every tick of the goal, as ZombieAttackGoal raises
	// the arms of a zombie.
	OnTick func(m *Mob, g *MeleeAttackGoal)
	// CheckAttack, if set, replaces checkAndPerformAttack, as goals that
	// override it do.
	CheckAttack func(m *Mob, g *MeleeAttackGoal, target world.Entity)

	path                     *mobPath
	pathedTarget             mgl64.Vec3
	ticksUntilNextPathRecalc int
	ticksUntilNextAttack     int
	lastCanUseCheck          int64
	hasLastCanUseCheck       bool
}

// NewMeleeAttackGoal returns a MeleeAttackGoal.
func NewMeleeAttackGoal(speedModifier float64, followEvenIfNotSeen bool) *MeleeAttackGoal {
	return &MeleeAttackGoal{goalFlags: goalFlags{GoalMove | GoalLook}, SpeedModifier: speedModifier, FollowingTargetEvenIfNotSeen: followEvenIfNotSeen}
}

func (g *MeleeAttackGoal) CanUse(m *Mob) bool {
	time := m.GameTime()
	if g.hasLastCanUseCheck && time-g.lastCanUseCheck < 20 {
		return false
	}
	g.lastCanUseCheck, g.hasLastCanUseCheck = time, true
	target := m.Target()
	if target == nil || !entityAlive(target) {
		return false
	}
	g.path = m.AI.nav.CreatePathToEntity(m, target, 0)
	return g.path != nil || m.WithinMeleeAttackRange(target)
}

func (g *MeleeAttackGoal) CanContinueToUse(m *Mob) bool {
	target := m.Target()
	if target == nil || !entityAlive(target) {
		return false
	}
	if !g.FollowingTargetEvenIfNotSeen {
		return !m.AI.nav.IsDone()
	}
	return m.WithinHome(cube.PosFromVec3(target.Position())) && !creativeOrSpectator(target)
}

func (g *MeleeAttackGoal) Start(m *Mob) {
	m.AI.nav.MoveTo(m, g.path, g.SpeedModifier)
	m.SetAggressive(true)
	g.ticksUntilNextPathRecalc, g.ticksUntilNextAttack = 0, 0
}

func (g *MeleeAttackGoal) Stop(m *Mob) {
	if target := m.Target(); target == nil || creativeOrSpectator(target) {
		m.SetTarget(nil)
	}
	m.SetAggressive(false)
	m.AI.nav.Stop()
}

func (g *MeleeAttackGoal) RequiresUpdateEveryTick() bool { return true }

func (g *MeleeAttackGoal) Tick(m *Mob) {
	defer func() {
		if g.OnTick != nil {
			g.OnTick(m, g)
		}
	}()
	target := m.Target()
	if target == nil {
		return
	}
	m.AI.look.SetLookAtEntityWith(target, 30, 30)
	g.ticksUntilNextPathRecalc = max(g.ticksUntilNextPathRecalc-1, 0)
	r := m.Random()
	if (g.FollowingTargetEvenIfNotSeen || m.HasLineOfSight(target)) && g.ticksUntilNextPathRecalc <= 0 &&
		(g.pathedTarget == (mgl64.Vec3{}) || target.Position().Sub(g.pathedTarget).LenSqr() >= 1 || r.NextFloat() < .05) {
		g.pathedTarget = target.Position()
		g.ticksUntilNextPathRecalc = 4 + int(r.NextInt(7))
		dist := m.DistanceToSqr(target)
		if dist > 1024 {
			g.ticksUntilNextPathRecalc += 10
		} else if dist > 256 {
			g.ticksUntilNextPathRecalc += 5
		}
		if !m.AI.nav.MoveToEntityReach(m, target, 0, g.SpeedModifier) {
			g.ticksUntilNextPathRecalc += 15
		}
		g.ticksUntilNextPathRecalc = adjustedTickDelay(g, g.ticksUntilNextPathRecalc)
	}
	g.ticksUntilNextAttack = max(g.ticksUntilNextAttack-1, 0)
	g.checkAndPerformAttack(m, target)
}

func (g *MeleeAttackGoal) checkAndPerformAttack(m *Mob, target world.Entity) {
	if g.CheckAttack != nil {
		g.CheckAttack(m, g, target)
		return
	}
	if g.CanPerformAttack(m, target) {
		g.ResetAttackCooldown()
		m.swing()
		m.doHurtTarget(target)
	}
}

// CanPerformAttack ports MeleeAttackGoal.canPerformAttack: the attack is
// ready and the target in reach and sight.
func (g *MeleeAttackGoal) CanPerformAttack(m *Mob, target world.Entity) bool {
	return g.ticksUntilNextAttack <= 0 && m.WithinMeleeAttackRange(target) && m.HasLineOfSight(target)
}

// ResetAttackCooldown waits 20 ticks for the next attack.
func (g *MeleeAttackGoal) ResetAttackCooldown() {
	g.ticksUntilNextAttack = adjustedTickDelay(g, 20)
}

// TicksUntilNextAttack returns the ticks until the next attack.
func (g *MeleeAttackGoal) TicksUntilNextAttack() int { return g.ticksUntilNextAttack }

// AttackInterval returns the ticks between attacks.
func (g *MeleeAttackGoal) AttackInterval() int { return adjustedTickDelay(g, 20) }

// swing shows the arm of the mob swinging to its viewers.
func (m *Mob) swing() {
	viewers := m.tx.Viewers(m.data.Pos)
	for _, v := range viewers {
		v.ViewEntityAction(m.Self, SwingArmAction{})
	}
	m.tx.ReleaseViewers(viewers)
}

// targetGoal holds what Java's TargetGoal shares between target goals.
type targetGoal struct {
	goalFlags
	mustSee, mustReach bool
	// followScale scales the follow range the goal looks for and keeps
	// targets within, as goals overriding getFollowDistance do.
	followScale       float64
	reachCache        int
	reachCacheTime    int
	unseenTicks       int
	targetMob         *world.EntityHandle
	unseenMemoryTicks int
}

func newTargetGoal(mustSee, mustReach bool) targetGoal {
	return targetGoal{goalFlags: goalFlags{GoalTarget}, mustSee: mustSee, mustReach: mustReach, unseenMemoryTicks: 60}
}

// followDistance ports TargetGoal.getFollowDistance.
func (g *targetGoal) followDistance(m *Mob) float64 {
	if g.followScale > 0 {
		return m.AI.FollowRange * g.followScale
	}
	return m.AI.FollowRange
}

// canContinue ports TargetGoal.canContinueToUse.
func (g *targetGoal) canContinue(m *Mob) bool {
	target := m.Target()
	if target == nil && g.targetMob != nil {
		target, _ = g.targetMob.Entity(m.tx)
	}
	if target == nil || !m.CanAttack(target) {
		return false
	}
	within := g.followDistance(m)
	if m.DistanceToSqr(target) > within*within {
		return false
	}
	if g.mustSee {
		if m.HasLineOfSight(target) {
			g.unseenTicks = 0
		} else if g.unseenTicks++; g.unseenTicks > reducedTickDelay(g.unseenMemoryTicks) {
			return false
		}
	}
	m.SetTarget(target)
	return true
}

func (g *targetGoal) start() {
	g.reachCache, g.reachCacheTime, g.unseenTicks = 0, 0, 0
}

func (g *targetGoal) stop(m *Mob) {
	m.SetTarget(nil)
	g.targetMob = nil
}

// canAttack ports TargetGoal.canAttack.
func (g *targetGoal) canAttack(m *Mob, target world.Entity, c targeting) bool {
	if target == nil || !c.test(m, target) || !m.WithinHome(cube.PosFromVec3(target.Position())) {
		return false
	}
	if g.mustReach {
		if g.reachCacheTime--; g.reachCacheTime <= 0 {
			g.reachCache = 0
		}
		if g.reachCache == 0 {
			g.reachCache = 2
			if g.canReach(m, target) {
				g.reachCache = 1
			}
		}
		if g.reachCache == 2 {
			return false
		}
	}
	return true
}

func (g *targetGoal) canReach(m *Mob, target world.Entity) bool {
	g.reachCacheTime = reducedTickDelay(10 + int(m.Random().NextInt(5)))
	path := m.AI.nav.CreatePathToEntity(m, target, 0)
	if path == nil || path.EndNode() == nil {
		return false
	}
	end := path.EndNode()
	pos := cube.PosFromVec3(target.Position())
	xx, zz := end.x-pos.X(), end.z-pos.Z()
	return float64(xx*xx+zz*zz) <= 2.25
}

// NearestAttackableTargetGoal targets the nearest entity of a type that may
// be attacked, ported from Java's NearestAttackableTargetGoal.
type NearestAttackableTargetGoal struct {
	targetGoal
	// IsType selects the entities targeted, players if nil.
	IsType         func(world.Entity) bool
	RandomInterval int
	Selector       func(m *Mob, target world.Entity) bool

	target *world.EntityHandle
}

// NewNearestAttackableTargetGoal returns a NearestAttackableTargetGoal for
// entities isType selects, or players if it is nil.
func NewNearestAttackableTargetGoal(isType func(world.Entity) bool, mustSee bool) *NearestAttackableTargetGoal {
	return &NearestAttackableTargetGoal{targetGoal: newTargetGoal(mustSee, false), IsType: isType, RandomInterval: reducedTickDelay(10)}
}

func (g *NearestAttackableTargetGoal) conditions(m *Mob) targeting {
	c := forCombat()
	c.rangeLimit = g.followDistance(m)
	c.selector = g.Selector
	return c
}

func (g *NearestAttackableTargetGoal) CanUse(m *Mob) bool {
	if g.RandomInterval > 0 && m.Random().NextInt(int32(g.RandomInterval)) != 0 {
		return false
	}
	g.target = nil
	var found world.Entity
	if g.IsType == nil {
		found = nearestPlayer(m, g.conditions(m))
	} else {
		r := g.followDistance(m)
		found = nearestOfTypes(m, g.conditions(m), m.BBox().GrowVec3(mgl64.Vec3{r, r, r}), g.IsType)
	}
	if found == nil {
		return false
	}
	g.target = found.H()
	return true
}

func (g *NearestAttackableTargetGoal) CanContinueToUse(m *Mob) bool { return g.canContinue(m) }

func (g *NearestAttackableTargetGoal) Start(m *Mob) {
	if g.target != nil {
		if t, ok := g.target.Entity(m.tx); ok {
			m.SetTarget(t)
		}
	}
	g.start()
}

func (g *NearestAttackableTargetGoal) Stop(m *Mob) { g.stop(m) }

// HurtByTargetGoal targets the entity that last hurt the mob, ported from
// Java's HurtByTargetGoal.
type HurtByTargetGoal struct {
	targetGoal
	// IgnoreDamageFrom selects attackers the mob doesn't fight back.
	IgnoreDamageFrom func(world.Entity) bool
	// AlertOthers alerts the mobs of the same type nearby, except those
	// AlertIgnore selects.
	AlertOthers bool
	AlertIgnore func(world.Entity) bool
	// AlertOther, if set, alerts a mob of the attacker in place of setting
	// its target, as HurtByTargetGoal.alertOther is overridden.
	AlertOther func(m, other *Mob, attacker world.Entity)

	timestamp int
}

// NewHurtByTargetGoal returns a HurtByTargetGoal.
func NewHurtByTargetGoal() *HurtByTargetGoal {
	// Java starts the timestamp at 0, which misses damage taken before the
	// first tick of the mob.
	return &HurtByTargetGoal{targetGoal: newTargetGoal(true, false), timestamp: -1}
}

func (g *HurtByTargetGoal) CanUse(m *Mob) bool {
	attacker, timestamp := m.LastHurtBy()
	if timestamp == g.timestamp || attacker == nil {
		return false
	}
	if _, player := attacker.(interface{ GameMode() world.GameMode }); player && m.tx.World().GameRuleBool("universal_anger") {
		return false
	}
	if g.IgnoreDamageFrom != nil && g.IgnoreDamageFrom(attacker) {
		return false
	}
	c := forCombat()
	c.ignoreSight, c.ignoreInvis = true, true
	return g.canAttack(m, attacker, c)
}

func (g *HurtByTargetGoal) CanContinueToUse(m *Mob) bool { return g.canContinue(m) }

func (g *HurtByTargetGoal) Start(m *Mob) {
	attacker, timestamp := m.LastHurtBy()
	m.SetTarget(attacker)
	if t := m.Target(); t != nil {
		g.targetMob = t.H()
	}
	g.timestamp = timestamp
	g.unseenMemoryTicks = 300
	if g.AlertOthers {
		g.alertOthers(m, attacker)
	}
	g.start()
}

func (g *HurtByTargetGoal) Stop(m *Mob) { g.stop(m) }

// alertOthers ports HurtByTargetGoal.alertOthers.
func (g *HurtByTargetGoal) alertOthers(m *Mob, attacker world.Entity) {
	if attacker == nil {
		return
	}
	within := m.AI.FollowRange
	pos := m.data.Pos
	box := cube.Box(pos[0], pos[1], pos[2], pos[0]+1, pos[1]+1, pos[2]+1).GrowVec3(mgl64.Vec3{within, 10, within})
	kind := m.H().Type().EncodeEntity()
	for e := range m.tx.EntitiesWithin(box) {
		if e.H() == m.H() || e.H().Type().EncodeEntity() != kind || g.AlertIgnore != nil && g.AlertIgnore(e) {
			continue
		}
		other, ok := mobOf(e)
		if !ok || other.Target() != nil || alliedTo(other, attacker) {
			continue
		}
		if g.AlertOther != nil {
			g.AlertOther(m, other, attacker)
			continue
		}
		other.SetTarget(attacker)
	}
}
