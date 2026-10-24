package entity

import (
	"math"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/loot"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// NewArmadillo creates an unrolled adult armadillo.
func NewArmadillo(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(ArmadilloType, ArmadilloConfig{})
}

// ArmadilloType is the registered Java armadillo type.
var ArmadilloType armadilloType

type armadilloType struct{}

func (armadilloType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	if d.Data == nil {
		ArmadilloConfig{}.Apply(d)
	}
	return &Armadillo{Ent: &Ent{tx: tx, handle: h, data: d}}
}
func (armadilloType) EncodeEntity() string { return "minecraft:armadillo" }
func (armadilloType) BBox(e world.Entity) cube.BBox {
	if a, ok := e.(*Armadillo); ok && a.behaviour().age < 0 {
		return cube.Box(-.21, 0, -.21, .21, .39, .21)
	}
	return cube.Box(-.35, 0, -.35, .35, .65, .35)
}
func (armadilloType) DecodeNBT(m map[string]any, d *world.EntityData) {
	s := ReadNativeMobJavaState(m)
	if s == nil {
		s = m
	}
	c := ArmadilloConfig{Age: int(nbtconv.Int32(s, "Age")), ScuteTicks: int(nbtconv.Int32(s, "scute_time"))}
	if c.ScuteTicks == 0 {
		c.ScuteTicks = int(nbtconv.Int32(s, "ScuteTime"))
	}
	switch state, _ := s["state"].(string); state {
	case "rolling":
		c.State = armadilloRolling
	case "scared":
		c.State = armadilloScared
	case "unrolling":
		c.State = armadilloUnrolling
	default:
		if nbtconv.Bool(s, "Rolling") {
			c.State = armadilloScared
		}
	}
	if a, ok := nativeMobAttribute(m, "minecraft:health"); ok {
		c.Health, c.MaxHealth = a.Current, a.Base
	}
	d.Data = c.New()
	d.Data.(*ArmadilloBehaviour).breeding.decodeNBT(s)
}
func (armadilloType) EncodeNBT(d *world.EntityData) map[string]any {
	b := d.Data.(*ArmadilloBehaviour)
	a := EncodeNativeMobAttributes(ReadNativeMobAttributes(d.NativeNBT), []NativeMobAttribute{{Name: "minecraft:health", Base: b.maxHealth, Current: b.health, DefaultMin: 0, DefaultMax: b.maxHealth, Min: 0, Max: b.maxHealth}})
	return MergeNativeMobJavaState(map[string]any{"Attributes": a}, MergeNativeMobNBT(ReadNativeMobJavaState(d.NativeNBT), b.breeding.encodeNBT(map[string]any{"Age": int32(b.age), "scute_time": int32(b.scuteTime), "state": armadilloStateNames[b.state]})))
}

// armadilloState ports Armadillo.ArmadilloState.
type armadilloState uint8

const (
	armadilloIdle armadilloState = iota
	armadilloRolling
	armadilloScared
	armadilloUnrolling
)

var armadilloStateNames = [...]string{"idle", "rolling", "scared", "unrolling"}

// animationDuration ports ArmadilloState.animationDuration.
func (s armadilloState) animationDuration() int64 { return [...]int64{0, 10, 50, 30}[s] }

func (c ArmadilloConfig) New() *ArmadilloBehaviour {
	if c.MaxHealth <= 0 {
		c.MaxHealth = 12
	}
	if c.Health <= 0 {
		c.Health = c.MaxHealth
	}
	if c.ScuteTicks == 0 {
		c.ScuteTicks = armadilloScuteDropTime(func(n int32) int32 { return int32(rand.IntN(int(n))) })
	}
	b := &ArmadilloBehaviour{health: c.Health, maxHealth: c.MaxHealth, effects: NewEffectManager(), age: c.Age, scuteTime: c.ScuteTicks, state: c.State, speed: .14}
	b.br = newArmadilloBrain(b)
	return b
}

// armadilloScuteDropTime ports Armadillo.pickNextScuteDropTime: five to ten
// minutes.
func armadilloScuteDropTime(next func(int32) int32) int { return int(next(6000)) + 6000 }

func (a *Armadillo) FinalizeSpawn(_ string, r *vanilla.Random) {
	if r != nil {
		a.behaviour().scuteTime = armadilloScuteDropTime(r.NextInt)
	}
}

// ArmadilloConfig configures age and saved scute and roll state.
type ArmadilloConfig struct {
	Health, MaxHealth float64
	Age, ScuteTicks   int
	State             armadilloState
}

func (c ArmadilloConfig) Apply(d *world.EntityData) { d.Data = c.New() }

// Armadillo is a passive animal that rolls up in its shell when it senses
// danger, and sheds scutes.
type Armadillo struct{ *Ent }
type ArmadilloBehaviour struct {
	health, maxHealth float64
	effects           *EffectManager
	ai                MobAI
	br                *Brain
	age, scuteTime    int
	state             armadilloState
	inStateTicks      int64
	// peekTicks counts down the peek of a scared armadillo viewers see.
	peekTicks int
	dead      bool
	speed     float64
	breeding  breeding
}

func (a *Armadillo) behaviour() *ArmadilloBehaviour { return a.Behaviour().(*ArmadilloBehaviour) }
func (a *Armadillo) Tick(tx *world.Tx, t int64)     { a.behaviour().effects.Tick(a, tx); a.Ent.Tick(tx, t) }

func (b *ArmadilloBehaviour) mobAI() *MobAI   { return &b.ai }
func (b *ArmadilloBehaviour) brain() *Brain   { return b.br }
func (b *ArmadilloBehaviour) scared() bool    { return b.state != armadilloIdle }
func (b *ArmadilloBehaviour) panicking() bool { return b.br.has(memIsPanicking) }

// Tick runs a tick of the armadillo: its ageing and love, and its MobAI with
// its brain.
func (b *ArmadilloBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	a := &Armadillo{Ent: e}
	b.ai.MovementSpeed, b.ai.EyeHeight = b.speed, .5525
	if b.age < 0 {
		b.ai.EyeHeight = .21875
	}
	if b.ai.started {
		// Armadillo.getMaxHeadYRot: a scared armadillo keeps its head still.
		b.ai.MaxHeadYaw = 32
		if b.scared() {
			b.ai.MaxHeadYaw = 1e-9
		}
	}
	tickAgeing(a, tx)
	return b.ai.Tick(e, a, func(m *Mob) { m.AI.nav.SetCanFloat(true) })
}

// customServerAiStep ports Armadillo.customServerAiStep: the brain ticks and
// picks the activity, and now and then the armadillo sheds a scute.
func (b *ArmadilloBehaviour) customServerAiStep(m *Mob) {
	b.br.tick(m)
	b.br.setActiveToFirstValid(activityPanic, activityIdle)
	if b.scuteTime--; b.scuteTime <= 0 && m.Alive() {
		if dropArmadilloShed(m.Ent, m.tx) {
			r := m.Random()
			m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "mob.armadillo.scute_drop", Volume: 1, Pitch: float64((r.NextFloat()-r.NextFloat())*.2 + 1)})
			m.tx.EmitGameEvent(world.GameEventEntityPlace, m.data.Pos, world.GameEventContext{Source: m.Self})
		}
		b.scuteTime = armadilloScuteDropTime(m.Random().NextInt)
	}
}

// afterAiStep ports what Armadillo.tick does after the tick of a LivingEntity:
// a scared armadillo turns its head with its body.
func (b *ArmadilloBehaviour) afterAiStep(m *Mob) {
	if b.scared() {
		m.AI.headYaw = m.AI.bodyYaw
	}
	b.inStateTicks++
	if b.peekTicks > 0 {
		if b.peekTicks--; b.peekTicks == 0 {
			m.updateState()
		}
	}
}

// switchState ports Armadillo.switchToState.
func (b *ArmadilloBehaviour) switchState(m *Mob, s armadilloState) {
	if b.state != s {
		b.state, b.inStateTicks = s, 0
		m.updateState()
	}
}

// rollUp ports Armadillo.rollUp: the armadillo stops, falls out of love and
// rolls up.
func (b *ArmadilloBehaviour) rollUp(m *Mob) {
	if b.scared() {
		return
	}
	m.AI.nav.Stop()
	m.AI.xxa, m.AI.zza = 0, 0
	m.SetSpeed(0)
	resetLove(m.Self.(breeder))
	m.tx.EmitGameEvent(world.GameEventEntityAction, m.data.Pos, world.GameEventContext{Source: m.Self})
	b.makeSound(m, "mob.armadillo.roll")
	b.switchState(m, armadilloRolling)
}

// rollOut ports Armadillo.rollOut.
func (b *ArmadilloBehaviour) rollOut(m *Mob) {
	if !b.scared() {
		return
	}
	m.tx.EmitGameEvent(world.GameEventEntityAction, m.data.Pos, world.GameEventContext{Source: m.Self})
	b.makeSound(m, "mob.armadillo.unroll_finish")
	b.switchState(m, armadilloIdle)
}

func (b *ArmadilloBehaviour) makeSound(m *Mob, name string) {
	r := m.Random()
	pitch := (r.NextFloat()-r.NextFloat())*.2 + 1
	if b.age < 0 {
		pitch = (r.NextFloat()-r.NextFloat())*.2 + 1.5
	}
	m.tx.PlaySound(m.data.Pos, sound.Custom{Name: name, Volume: 1, Pitch: float64(pitch)})
}

// canStayRolledUp ports Armadillo.canStayRolledUp.
func (b *ArmadilloBehaviour) canStayRolledUp(m *Mob) bool {
	return !b.panicking() && !m.InLiquid() && m.data.Riding == nil && m.HolderLeashID() == 0 && !isVehicle(m.Self)
}

// scaredBy ports Armadillo.isScaredBy: the undead, what hurt it and players
// sprinting or riding within seven blocks scare it.
func (b *ArmadilloBehaviour) scaredBy(m *Mob, e world.Entity) bool {
	if !m.BBox().GrowVec3(mgl64.Vec3{7, 2, 7}).IntersectsWith(e.H().Type().BBox(e).Translate(e.Position())) {
		return false
	}
	if witherFriend(e) {
		return true
	}
	if attacker, _ := m.LastHurtBy(); attacker != nil && attacker.H() == e.H() {
		return true
	}
	if isPlayerEntity(e) && !spectator(e) {
		s, ok := e.(interface{ Sprinting() bool })
		return ok && s.Sprinting() || isPassenger(e)
	}
	return false
}

// newArmadilloBrain ports ArmadilloAi and the brain provider of Armadillo.
func newArmadilloBrain(a *ArmadilloBehaviour) *Brain {
	b := newBrain(nearestLivingEntitySensor{}, hurtBySensor{}, temptingSensor{items: animalTemptation}, adultSensor{},
		mobSensor{rate: 5, test: a.scaredBy, ready: a.canStayRolledUp, mem: memDangerDetectedRecently, ttl: 80})
	rollingOut := newOneShot([]memoryCondition{{memDangerDetectedRecently, memAbsent}}, func(m *Mob, _ *Brain, _ int64) bool {
		if !a.scared() {
			return false
		}
		a.rollOut(m)
		return true
	})
	b.addActivity(activityCore, 0,
		newSwim(.8),
		newAnimalPanic(2, panicEnvironmentalCause, nil, a.rollOut),
		newLookAtTargetSink(45, 90),
		newMoveToTargetSink(150, 250, func(*Mob) bool { return !a.scared() }),
		newCountDownCooldownTicks(memTemptationCooldownTicks),
		newCountDownCooldownTicks(memGazeCooldownTicks),
		rollingOut,
	)
	b.addActivityWith(activityIdle, []prioritizedBehavior{
		{0, newSetEntityLookTargetSometimes(isPlayerType, 6, uniformInt{30, 60})},
		{1, newAnimalMakeLove(ArmadilloType, 1, 1)},
		{2, newRunOne(nil,
			weightedBehavior{newFollowTemptation(constSpeed(1.25), func(m *Mob) float64 {
				if mobBaby(m.Self) {
					return 1
				}
				return 2
			}, false), 1},
			weightedBehavior{newBabyFollowAdult(uniformInt{5, 16}, constSpeed(1.25), memNearestVisibleAdult, false), 1},
		)},
		{3, newRandomLookAroundBehavior(uniformInt{150, 250}, 30, 0, 0)},
		{4, newRunOne([]memoryCondition{{memWalkTarget, memAbsent}},
			weightedBehavior{newRandomStroll(1, true), 1},
			weightedBehavior{newSetWalkTargetFromLookTarget(nil, constSpeed(1), 3), 1},
			weightedBehavior{newDoNothing(30, 60), 1},
		)},
	}, nil, nil)
	b.addActivityWith(activityPanic, []prioritizedBehavior{{0, newArmadilloBallUp(a)}},
		[]memoryCondition{{memDangerDetectedRecently, memPresent}, {memIsPanicking, memAbsent}}, nil)
	return b
}

// newArmadilloBallUp ports ArmadilloAi.ArmadilloBallUp: an armadillo on the
// ground that senses danger rolls up for up to five minutes, peeks out now
// and then while the danger is around, and unrolls once it has been gone.
func newArmadilloBallUp(a *ArmadilloBehaviour) *behavior {
	nextPeek, dangerWasAround := 0, false
	pickPeek := func(m *Mob) int { return int(armadilloScared.animationDuration()) + 100 + int(m.Random().NextInt(301)) }
	return &behavior{
		minDuration: 6000, maxDuration: 6000,
		checkExtra:  func(m *Mob) bool { return m.OnGround() && !m.InLiquid() },
		canStillUse: func(*Mob, int64) bool { return a.state != armadilloIdle },
		start:       func(m *Mob, _ int64) { a.rollUp(m) },
		stop: func(m *Mob, _ int64) {
			if !a.canStayRolledUp(m) {
				a.rollOut(m)
			}
		},
		tick: func(m *Mob, _ int64) {
			if nextPeek > 0 {
				nextPeek--
			}
			if a.state == armadilloRolling && a.inStateTicks > armadilloRolling.animationDuration() {
				a.switchState(m, armadilloScared)
				if m.OnGround() {
					m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "mob.armadillo.land", Volume: 1, Pitch: 1})
				}
				return
			}
			danger := a.br.timeUntilExpiry(memDangerDetectedRecently)
			around := danger > 75
			if around != dangerWasAround {
				nextPeek = pickPeek(m)
			}
			dangerWasAround = around
			switch a.state {
			case armadilloScared:
				if nextPeek == 0 && m.OnGround() && around {
					// Entity event 64 makes the armadillo peek out.
					a.peekTicks = int(armadilloScared.animationDuration())
					m.updateState()
					m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "mob.armadillo.peek", Volume: 1, Pitch: 1})
					nextPeek = pickPeek(m)
				}
				if danger < armadilloUnrolling.animationDuration() {
					m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "mob.armadillo.unroll_start", Volume: 1, Pitch: 1})
					a.switchState(m, armadilloUnrolling)
				}
			case armadilloUnrolling:
				if danger > armadilloUnrolling.animationDuration() {
					a.switchState(m, armadilloScared)
				}
			}
		},
	}
}

func dropArmadilloShed(e *Ent, tx *world.Tx) bool {
	stacks, err := loot.Generate("minecraft:gameplay/armadillo_shed", rand.Int64(), loot.Context{Entity: "minecraft:armadillo", Origin: e.Position()})
	if err != nil || len(stacks) == 0 {
		return false
	}
	for _, stack := range stacks {
		tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: e.Position()}, stack))
	}
	return true
}
func (a *Armadillo) Health() float64    { return a.behaviour().health }
func (a *Armadillo) MaxHealth() float64 { return a.behaviour().maxHealth }
func (a *Armadillo) SetMaxHealth(v float64) {
	b := a.behaviour()
	b.maxHealth = v
	b.health = math.Min(v, b.health)
}
func (a *Armadillo) Dead() bool { return a.behaviour().dead || a.Health() <= 0 }

// Panicking reports whether the armadillo runs from danger.
func (a *Armadillo) Panicking() bool { return a.behaviour().panicking() }

// Hurt ports Armadillo.hurtServer and actuallyHurt: a scared armadillo takes
// half of a point less than the damage, and a blow from a living thing makes
// it sense danger and roll up, while fire and the like make it unroll.
func (a *Armadillo) Hurt(v float64, src world.DamageSource) (float64, bool) {
	b := a.behaviour()
	if a.Dead() || v <= 0 || (src.Fire() && a.hasEffect(effect.FireResistance)) {
		return 0, false
	}
	if b.scared() {
		v = (v - 1) / 2
	}
	v, ok := b.ai.DamageCooldown(a.tx, v)
	if !ok {
		return 0, false
	}
	if resistance, ok := b.effects.Effect(effect.Resistance); ok {
		v *= effect.Resistance.Multiplier(src, resistance.Level())
	}
	b.health -= math.Max(0, v)
	b.ai.Hurt(src)
	resetLove(a)
	a.viewAction(HurtAction{})
	onMobHurt(a.tx, a)
	if a.Dead() {
		b.dead = true
		a.viewAction(DeathAction{})
		dropAllDeathLoot(a.tx, a, src)
		_ = a.CloseIn(a.tx)
		return v, true
	}
	m := b.ai.Mob(a.Ent, a)
	if attacker := mobKiller(src); attacker != nil {
		if _, living := attacker.(Living); living {
			b.br.setWithExpiry(memDangerDetectedRecently, true, 80)
			if b.canStayRolledUp(m) {
				b.rollUp(m)
			}
		}
	} else if panicEnvironmentalCause(src) {
		b.rollOut(m)
	}
	return v, true
}
func (a *Armadillo) hasEffect(t effect.Type) bool {
	_, ok := a.behaviour().effects.Effect(t)
	return ok
}
func (a *Armadillo) AddEffect(e effect.Effect)  { a.behaviour().effects.Add(e, a) }
func (a *Armadillo) RemoveEffect(t effect.Type) { a.behaviour().effects.Remove(t, a) }
func (a *Armadillo) Effects() []effect.Effect   { return a.behaviour().effects.Effects() }
func (a *Armadillo) Speed() float64             { return a.behaviour().speed }
func (a *Armadillo) SetSpeed(v float64)         { a.behaviour().speed = v }
func (a *Armadillo) Heal(v float64, _ world.HealingSource) float64 {
	if a.Dead() || v <= 0 {
		return 0
	}
	b := a.behaviour()
	old := b.health
	b.health = math.Min(b.maxHealth, b.health+v)
	return b.health - old
}
func (a *Armadillo) KnockBack(src mgl64.Vec3, f, h float64) {
	d := a.Position().Sub(src)
	d[1] = 0
	if d.Len() > 0 {
		d = d.Normalize().Mul(f)
	}
	d[1] = h
	a.SetVelocity(d)
}
func (a *Armadillo) viewAction(x world.EntityAction) {
	vs := a.tx.Viewers(a.Position())
	for _, v := range vs {
		v.ViewEntityAction(a, x)
	}
	a.tx.ReleaseViewers(vs)
}

// Rolling reports if the armadillo is rolled up in any way.
func (a *Armadillo) Rolling() bool { return a.behaviour().scared() }

// BedrockState returns the armadillo_state property of Bedrock: unrolled,
// rolled_up, rolled_up_peeking or rolled_up_unrolling.
func (a *Armadillo) BedrockState() string {
	b := a.behaviour()
	switch {
	case b.state == armadilloIdle:
		return "unrolled"
	case b.state == armadilloUnrolling:
		return "rolled_up_unrolling"
	case b.peekTicks > 0:
		return "rolled_up_peeking"
	}
	return "rolled_up"
}

// Brush ports Armadillo.brushOffScute: an adult drops a scute.
func (a *Armadillo) Brush() item.Stack {
	if a.Dead() || a.behaviour().age < 0 {
		return item.Stack{}
	}
	stacks, err := loot.Generate("minecraft:brush/armadillo", rand.Int64(), loot.Context{Entity: "minecraft:armadillo", Origin: a.Position()})
	if err != nil || len(stacks) == 0 {
		return item.Stack{}
	}
	a.tx.PlaySound(a.Position(), sound.Custom{Name: "mob.armadillo.brush", Volume: 1, Pitch: 1})
	a.tx.EmitGameEvent(world.GameEventEntityInteract, a.Position(), world.GameEventContext{Source: a})
	return stacks[0]
}

// Interact ports Armadillo.mobInteract: a scared armadillo takes nothing,
// and spider eyes breed it or let a pup grow up faster.
func (a *Armadillo) Interact(_ *world.Tx, u item.User, c *item.UseContext) bool {
	return !a.behaviour().scared() && feedBreeder(a, u, c)
}

// canFallInLove ports Armadillo.canFallInLove.
func (a *Armadillo) canFallInLove() bool { return !a.behaviour().scared() }

// InLove reports whether the armadillo is in love.
func (a *Armadillo) InLove() bool { return a.behaviour().breeding.InLove() }

func (a *Armadillo) breedingState() *breeding { return &a.behaviour().breeding }
func (a *Armadillo) ageTicks() *int           { return &a.behaviour().age }

func (a *Armadillo) breedingFood(stack item.Stack) bool {
	return itemNamed(stack, "minecraft:spider_eye")
}

func (a *Armadillo) breedOffspring(_ *world.Tx, _ breeder) *world.EntityHandle {
	return babyAt(a).New(ArmadilloType, ArmadilloConfig{Age: babyStartAge})
}

var _ Interactable = (*Armadillo)(nil)

// walkTargetValue ports Animal.getWalkTargetValue.
func (b *ArmadilloBehaviour) walkTargetValue(m *Mob, pos cube.Pos) float64 {
	return animalWalkTargetValue(m, pos)
}
