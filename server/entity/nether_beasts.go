package entity

import (
	"math"
	"time"

	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/entity/effect"
	"github.com/dm-vev/adamant/server/internal/nbtconv"
	"github.com/dm-vev/adamant/server/internal/vanilla"
	"github.com/dm-vev/adamant/server/item"
	"github.com/dm-vev/adamant/server/world"
	"github.com/dm-vev/adamant/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// Each nether beast has a distinct native type and concrete public entity.
type netherBeastKind uint8

const (
	hoglinKind netherBeastKind = iota
	zoglinKind
)

type netherBeastState struct {
	health     *HealthManager
	kind       netherBeastKind
	effects    *EffectManager
	ai         MobAI
	br         *brain
	age        int
	conversion int
	// attackAnimation is HoglinBase.getAttackAnimationRemainingTicks.
	attackAnimation int
	immune, dead    bool
	cannotBeHunted  bool
	speed           float64
	breeding        breeding
}
type netherBeastConfig struct {
	kind           netherBeastKind
	health         float64
	age            int
	immune         bool
	cannotBeHunted bool
	conversion     int
}

func (c netherBeastConfig) Apply(d *world.EntityData) {
	h := 40.0
	if c.health > 0 {
		h = c.health
	}
	s := &netherBeastState{kind: c.kind, health: NewHealthManager(h, 40), effects: NewEffectManager(), age: c.age, immune: c.immune, cannotBeHunted: c.cannotBeHunted, conversion: c.conversion, speed: .3}
	if c.kind == hoglinKind {
		s.br = newHoglinBrain(s)
	} else {
		s.br = newZoglinBrain()
	}
	d.Data = s
}
func (t netherBeastType) EncodeEntity() string { return t.id }
func (t netherBeastType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	if d.Data == nil {
		netherBeastConfig{kind: t.kind}.Apply(d)
	}
	e := &Ent{tx: tx, handle: h, data: d}
	if t.kind == zoglinKind {
		return &zoglin{e}
	}
	return &hoglin{e}
}
func (t netherBeastType) BBox(e world.Entity) cube.BBox {
	small := false
	if a, ok := e.(interface{ beast() *netherBeastState }); ok {
		small = a.beast().age < 0
	}
	if small {
		return cube.Box(-.375, 0, -.375, .375, .85, .375)
	}
	return cube.Box(-.7, 0, -.7, .7, 1.4, .7)
}
func (t netherBeastType) AttributeDefaults() []nativeMobAttr {
	health, speed, damage := 40.0, .3, 6.0
	return []nativeMobAttr{{Name: "minecraft:health", Base: health, Current: health, DefaultMin: 0, DefaultMax: health, Min: 0, Max: health}, {Name: "minecraft:movement", Base: speed, Current: speed, DefaultMin: 0, DefaultMax: speed, Min: 0, Max: speed}, {Name: "minecraft:attack_damage", Base: damage, Current: damage, DefaultMin: 0, DefaultMax: damage, Min: 0, Max: damage}}
}

type hoglin struct{ *Ent }
type zoglin struct{ *Ent }

func (h *hoglin) beast() *netherBeastState { return h.Behaviour().(*netherBeastState) }
func (z *zoglin) beast() *netherBeastState { return z.Behaviour().(*netherBeastState) }
func (t netherBeastType) DecodeNBT(n map[string]any, d *world.EntityData) {
	j := readNativeMobJavaState(n)
	if j == nil {
		j = n
	}
	age := int(nbtconv.Int32(j, "Age"))
	if age == 0 && nbtconv.Bool(j, "IsBaby") {
		age = -24000
	}
	c := netherBeastConfig{kind: t.kind, age: age, immune: nbtconv.Bool(n, "IsImmuneToZombification"), cannotBeHunted: nbtconv.Bool(j, "CannotBeHunted"), conversion: int(nbtconv.Int32(n, "TimeInOverworld"))}
	if x, ok := nativeMobAttribute(n, "minecraft:health"); ok {
		c.health = x.Current
	} else if _, ok := j["Health"]; ok {
		c.health = float64(nbtconv.Float32(j, "Health"))
	}
	c.Apply(d)
	s := d.Data.(*netherBeastState)
	s.breeding.decodeNBT(j)
}
func (t netherBeastType) EncodeNBT(d *world.EntityData) map[string]any {
	s := d.Data.(*netherBeastState)
	attrs := t.AttributeDefaults()
	attrs[0].Current = s.health.Health()
	attrs[1].Current = s.speed
	native := map[string]any{"Attributes": encodeNativeMobAttributes(readNativeMobAttributes(d.NativeNBT), attrs), "IsBaby": byte(boolByte(s.age < 0))}
	if t.kind == hoglinKind {
		native["IsImmuneToZombification"] = byte(boolByte(s.immune))
		native["TimeInOverworld"] = int32(s.conversion)
	}
	out := mergeNativeMobNBT(d.NativeNBT, native)
	j := readNativeMobJavaState(d.NativeNBT)
	if j == nil {
		j = make(map[string]any)
	}
	j["Age"] = int32(s.age)
	delete(j, "AttackCooldown")
	if t.kind == hoglinKind {
		j["CannotBeHunted"] = byte(boolByte(s.cannotBeHunted))
	}
	if t.kind != zoglinKind {
		s.breeding.encodeNBT(j)
	}
	return mergeNativeMobJavaState(out, j)
}
func (h *hoglin) SpeciesType() world.EntityType { return hoglinType }
func (z *zoglin) SpeciesType() world.EntityType { return zoglinType }
func (h *hoglin) Tick(tx *world.Tx, n int64)    { h.beast().effects.Tick(h, tx); h.Ent.Tick(tx, n) }
func (z *zoglin) Tick(tx *world.Tx, n int64)    { z.beast().effects.Tick(z, tx); z.Ent.Tick(tx, n) }
func (s *netherBeastState) mobAI() *MobAI       { return &s.ai }
func (s *netherBeastState) brain() *brain       { return s.br }

// self returns the entity of the beast.
func (s *netherBeastState) self(e *Ent) world.Entity {
	if s.kind == zoglinKind {
		return &zoglin{e}
	}
	return &hoglin{e}
}

// Tick runs a tick of the beast: the ageing and love of a hoglin, and its
// MobAI with its brain.
func (s *netherBeastState) Tick(e *Ent, tx *world.Tx) *Movement {
	self := s.self(e)
	s.ai.MovementSpeed = s.speed
	if s.age < 0 {
		// Hoglin.BABY_DIMENSIONS.
		s.ai.EyeHeight = .625
	} else {
		s.ai.EyeHeight = 0
	}
	if s.kind == hoglinKind {
		tickAgeing(self.(*hoglin), tx)
	}
	return s.ai.Tick(e, self, nil)
}

// customServerAiStep ports Hoglin.customServerAiStep and Zoglin's.
func (s *netherBeastState) customServerAiStep(m *Mob) {
	old, _ := s.br.activeNonCore()
	s.br.tick(m)
	if s.kind == zoglinKind {
		// Zoglin.updateActivity.
		s.br.setActiveToFirstValid(activityFight, activityIdle)
		if now, _ := s.br.activeNonCore(); now == activityFight && old != activityFight {
			s.makeSound(m, "entity.zoglin.angry")
		}
		m.SetAggressive(s.br.has(memAttackTarget))
		return
	}
	// HoglinAi.updateActivity.
	s.br.setActiveToFirstValid(activityFight, activityAvoid, activityIdle)
	if now, _ := s.br.activeNonCore(); now != old {
		s.makeSound(m, s.activitySound(m))
	}
	m.SetAggressive(s.br.has(memAttackTarget))
	if s.converting(m) {
		if s.conversion++; s.conversion > 300 {
			s.makeSound(m, "entity.hoglin.converted_to_zombified")
			s.finishConversion(m)
		}
	} else {
		s.conversion = 0
	}
}

// afterAiStep counts down the attack animation of the beast, as aiStep.
func (s *netherBeastState) afterAiStep(*Mob) {
	if s.attackAnimation > 0 {
		s.attackAnimation--
	}
}

// makeSound ports Mob.makeSound.
func (s *netherBeastState) makeSound(m *Mob, name string) {
	r := m.Random()
	pitch := float64((r.NextFloat()-r.NextFloat())*.2 + 1)
	if s.age < 0 {
		pitch = float64((r.NextFloat()-r.NextFloat())*.2 + 1.5)
	}
	m.tx.PlaySound(m.data.Pos, sound.Custom{Name: name, Volume: 1, Pitch: pitch})
}

// activitySound ports HoglinAi.getSoundForActivity.
func (s *netherBeastState) activitySound(m *Mob) string {
	act, _ := s.br.activeNonCore()
	switch {
	case act == activityAvoid || s.converting(m):
		return "entity.hoglin.retreat"
	case act == activityFight:
		return "entity.hoglin.angry"
	case s.br.has(memNearestRepellent):
		return "entity.hoglin.retreat"
	}
	return "entity.hoglin.ambient"
}

// converting ports Hoglin.isConverting: a hoglin out of the nether turns
// into a zoglin.
func (s *netherBeastState) converting(m *Mob) bool {
	return !s.immune && !m.AI.NoAI && m.tx.World().Dimension() == world.Overworld
}

// finishConversion ports Hoglin.finishConversion: the hoglin becomes a
// zoglin, sick for ten seconds.
func (s *netherBeastState) finishConversion(m *Mob) {
	opts := world.EntitySpawnOpts{Position: m.data.Pos, Rotation: m.data.Rot, SpawnReason: "conversion", SpawnFinalized: true}
	z := convertMob(m.tx, m.Self, opts.New(zoglinType, netherBeastConfig{kind: zoglinKind, age: min(s.age, 0)}), conversion{keepEquipment: true})
	z.(*zoglin).AddEffect(effect.New(effect.Nausea, 1, 10*time.Second))
	s.dead = true
}

// walkTargetValue ports Hoglin.getWalkTargetValue: hoglins keep away from
// repellents and like crimson nylium. Zoglins have no preference.
func (s *netherBeastState) walkTargetValue(m *Mob, pos cube.Pos) float64 {
	if s.kind == zoglinKind {
		return 0
	}
	if v, ok := s.br.get(memNearestRepellent); ok && distSqrPos(v.(cube.Pos), pos) < 64 {
		return -1
	}
	if name, _ := m.tx.Block(pos.Side(cube.FaceDown)).EncodeBlock(); name == "minecraft:crimson_nylium" {
		return 10
	}
	return 0
}

// doHurtTarget ports Hoglin.doHurtTarget and Zoglin's: the beast gores its
// target for half to one and a half times its attack damage and tosses it.
func (s *netherBeastState) doHurtTarget(m *Mob, target world.Entity) bool {
	l, ok := target.(Living)
	if !ok {
		return false
	}
	s.attackAnimation = 10
	if s.kind == zoglinKind {
		s.makeSound(m, "entity.zoglin.attack")
	} else {
		s.makeSound(m, "entity.hoglin.attack")
		hoglinOnHitTarget(m, s, target)
	}
	damage := 6.0
	if s.age < 0 {
		damage = .5
	} else {
		damage = damage/2 + float64(m.Random().NextInt(int32(damage)))
	}
	if _, hurt := l.Hurt(damage, AttackDamageSource{Attacker: m.Self}); !hurt {
		return false
	}
	if s.age >= 0 {
		hoglinThrowTarget(m, target)
	}
	return true
}

// hoglinThrowTarget ports HoglinBase.throwTarget: an adult tosses what it
// hits up and away, less so what resists knockback.
func hoglinThrowTarget(m *Mob, target world.Entity) {
	power := 1 - entityKnockbackResistance(target)
	if power <= 0 {
		return
	}
	v, ok := target.(interface {
		Velocity() mgl64.Vec3
		SetVelocity(mgl64.Vec3)
	})
	if !ok {
		return
	}
	r := m.Random()
	angle := float64(r.NextInt(21)-10) * math.Pi / 180
	horizontal := power * float64(r.NextFloat()*.5+.2)
	d := mgl64.Vec3{target.Position()[0] - m.data.Pos[0], 0, target.Position()[2] - m.data.Pos[2]}
	if d.LenSqr() > 0 {
		d = d.Normalize().Mul(horizontal)
	}
	// Vec3.yRot.
	cos, sin := math.Cos(angle), math.Sin(angle)
	push := mgl64.Vec3{d[0]*cos + d[2]*sin, power * float64(r.NextFloat()) * .5, d[2]*cos - d[0]*sin}
	v.SetVelocity(v.Velocity().Add(push))
}

// KnockbackResistance returns the knockback resistance of the hoglin.
func (h *hoglin) KnockbackResistance() float64 { return .6 }

// KnockbackResistance returns the knockback resistance of the zoglin.
func (z *zoglin) KnockbackResistance() float64 { return .6 }

// AttackAnimation returns the ticks left of the attack animation of the
// hoglin.
func (h *hoglin) AttackAnimation() int { return h.beast().attackAnimation }

// newHoglinBrain ports HoglinAi and the brain provider of Hoglin.
func newHoglinBrain(s *netherBeastState) *brain {
	br := newBrain(nearestLivingEntitySensor{}, playerSensor{}, adultSensor{}, hoglinSpecificSensor{})
	adult := func(*Mob) bool { return s.age >= 0 }
	idleMovement := func() behaviorControl {
		return newRunOne(nil,
			weightedBehavior{newRandomStroll(.4, true), 2},
			weightedBehavior{newSetWalkTargetFromLookTarget(nil, constSpeed(.4), 3), 2},
			weightedBehavior{newDoNothing(30, 60), 1},
		)
	}
	br.addActivity(activityCore, 0, newLookAtTargetSink(45, 90), newMoveToTargetSink(150, 250, nil))
	br.addActivity(activityIdle, 10,
		newBecomePassiveIfMemoryPresent(memNearestRepellent, 200),
		newAnimalMakeLove(hoglinType, .6, 2),
		newSetWalkTargetAwayFrom(memNearestRepellent, 1, 8, true, func(m *Mob, v any) mgl64.Vec3 {
			return v.(cube.Pos).Vec3Middle()
		}),
		newStartAttacking(nil, func(m *Mob) world.Entity {
			if br := m.brain(); br.has(memPacified) || br.has(memBreedTarget) {
				return nil
			}
			return m.brain().entity(m, memNearestVisibleAttackablePlayer)
		}),
		newTriggerIf(adult, newSetWalkTargetAwayFrom(memNearestVisibleAdultPiglin, .4, 8, false, entityMemoryPos)),
		newSetEntityLookTargetSometimes(nil, 8, uniformInt{30, 60}),
		newBabyFollowAdult(uniformInt{5, 16}, constSpeed(.6), memNearestVisibleAdult, false),
		idleMovement(),
	)
	br.addActivityWith(activityFight, prioritiesFrom(10,
		newBecomePassiveIfMemoryPresent(memNearestRepellent, 200),
		newAnimalMakeLove(hoglinType, .6, 2),
		newSetWalkTargetFromAttackTargetIfTargetOutOfReach(constSpeed(1)),
		newTriggerIf(adult, newMeleeAttack(40)),
		newTriggerIf(func(*Mob) bool { return s.age < 0 }, newMeleeAttack(15)),
		newStopAttackingIfTargetInvalid(nil, nil, true),
		newEraseMemoryIf(isBreeding, memAttackTarget),
	), []memoryCondition{{memAttackTarget, memPresent}}, []memoryType{memAttackTarget})
	br.addActivityWith(activityAvoid, prioritiesFrom(10,
		newSetWalkTargetAwayFrom(memAvoidTarget, 1.3, 15, false, entityMemoryPos),
		idleMovement(),
		newSetEntityLookTargetSometimes(nil, 8, uniformInt{30, 60}),
		newEraseMemoryIf(func(m *Mob) bool { return s.age >= 0 && !hoglinPiglinsOutnumber(m, s) }, memAvoidTarget),
	), []memoryCondition{{memAvoidTarget, memPresent}}, []memoryType{memAvoidTarget})
	return br
}

// newZoglinBrain ports the brain of Zoglin.
func newZoglinBrain() *brain {
	br := newBrain(nearestLivingEntitySensor{}, playerSensor{})
	br.addActivity(activityCore, 0, newLookAtTargetSink(45, 90), newMoveToTargetSink(150, 250, nil))
	br.addActivity(activityIdle, 10,
		newStartAttacking(nil, func(m *Mob) world.Entity {
			v := visibleLiving(m)
			if v == nil {
				return nil
			}
			return v.findClosest(m, func(e world.Entity) bool {
				return e.H().Type() != zoglinType && e.H().Type().EncodeEntity() != "minecraft:creeper" && sensorAttackable(m, e)
			})
		}),
		newSetEntityLookTargetSometimes(nil, 8, uniformInt{30, 60}),
		newRunOne(nil,
			weightedBehavior{newRandomStroll(.4, true), 2},
			weightedBehavior{newSetWalkTargetFromLookTarget(nil, constSpeed(.4), 3), 2},
			weightedBehavior{newDoNothing(30, 60), 1},
		),
	)
	br.addActivityWith(activityFight, prioritiesFrom(10,
		newSetWalkTargetFromAttackTargetIfTargetOutOfReach(constSpeed(1)),
		newTriggerIf(func(m *Mob) bool { return !mobBaby(m.Self) }, newMeleeAttack(40)),
		newTriggerIf(func(m *Mob) bool { return mobBaby(m.Self) }, newMeleeAttack(15)),
		newStopAttackingIfTargetInvalid(nil, nil, true),
	), []memoryCondition{{memAttackTarget, memPresent}}, []memoryType{memAttackTarget})
	return br
}

// prioritiesFrom pairs behaviours with priorities counting up from first,
// as ActivityData.createPriorityPairs.
func prioritiesFrom(first int, behaviors ...behaviorControl) []prioritizedBehavior {
	pairs := make([]prioritizedBehavior, len(behaviors))
	for i, b := range behaviors {
		pairs[i] = prioritizedBehavior{first + i, b}
	}
	return pairs
}

// entityMemoryPos returns the position of the entity a memory holds.
func entityMemoryPos(m *Mob, v any) mgl64.Vec3 {
	if e, ok := v.(*world.EntityHandle).Entity(m.tx); ok {
		return e.Position()
	}
	return m.data.Pos
}

// hoglinSpecificSensor ports HoglinSpecificSensor: the nearest repellent,
// and the adult piglins and hoglins the hoglin sees.
type hoglinSpecificSensor struct{}

func (hoglinSpecificSensor) scanRate() int { return 0 }
func (hoglinSpecificSensor) requires() []memoryType {
	return []memoryType{memNearestVisibleLivingEntities, memNearestRepellent, memNearestVisibleAdultPiglin, memNearestVisibleAdultHoglins,
		memVisibleAdultPiglinCount, memVisibleAdultHoglinCount}
}

func (hoglinSpecificSensor) doTick(m *Mob, b *brain) {
	if p, ok := findManhattanBox(m.BlockPos(), 8, 4, func(p cube.Pos) bool { return blockInTag(m.tx.Block(p), "hoglin_repellents") }); ok {
		b.set(memNearestRepellent, p)
	} else {
		b.erase(memNearestRepellent)
	}
	var piglin world.Entity
	piglins := 0
	var hoglins []*world.EntityHandle
	if v := visibleLiving(m); v != nil {
		for _, e := range v.findAll(m, func(e world.Entity) bool { return !mobBaby(e) }) {
			switch e.H().Type().EncodeEntity() {
			case "minecraft:piglin":
				piglins++
				if piglin == nil {
					piglin = e
				}
			case "minecraft:hoglin":
				hoglins = append(hoglins, e.H())
			}
		}
	}
	b.setEntity(memNearestVisibleAdultPiglin, piglin)
	b.set(memNearestVisibleAdultHoglins, hoglins)
	b.set(memVisibleAdultPiglinCount, piglins)
	b.set(memVisibleAdultHoglinCount, len(hoglins))
}

// hoglinPiglinsOutnumber ports HoglinAi.piglinsOutnumberHoglins.
func hoglinPiglinsOutnumber(m *Mob, s *netherBeastState) bool {
	if s.age < 0 {
		return false
	}
	piglins, _ := m.brain().int(memVisibleAdultPiglinCount)
	hoglins, _ := m.brain().int(memVisibleAdultHoglinCount)
	return piglins > hoglins+1
}

// visibleAdultHoglins returns the adult hoglins the hoglin sees.
func visibleAdultHoglins(m *Mob) []*Mob {
	v, _ := m.brain().get(memNearestVisibleAdultHoglins)
	handles, _ := v.([]*world.EntityHandle)
	var out []*Mob
	for _, h := range handles {
		if e, ok := h.Entity(m.tx); ok {
			if hm, ok := brainMobOf(e); ok {
				out = append(out, hm)
			}
		}
	}
	return out
}

// hoglinOnHitTarget ports HoglinAi.onHitTarget: an adult hoglin that hits a
// piglin while piglins outnumber hoglins retreats with its herd, and
// otherwise calls its herd to the fight.
func hoglinOnHitTarget(m *Mob, s *netherBeastState, target world.Entity) {
	if s.age < 0 {
		return
	}
	if target.H().Type().EncodeEntity() == "minecraft:piglin" && hoglinPiglinsOutnumber(m, s) {
		hoglinSetAvoidTarget(m, target)
		for _, h := range visibleAdultHoglins(m) {
			hoglinRetreatFrom(h, target)
		}
		return
	}
	hoglinBroadcastAttackTarget(m, target)
}

// hoglinRetreatFrom ports HoglinAi.retreatFromNearestTarget.
func hoglinRetreatFrom(m *Mob, avoid world.Entity) {
	b := m.brain()
	nearest := nearestOf(m, b.entity(m, memAvoidTarget), avoid)
	nearest = nearestOf(m, b.entity(m, memAttackTarget), nearest)
	hoglinSetAvoidTarget(m, nearest)
}

// nearestOf ports BehaviorUtils.getNearestTarget: of a possible target and
// another, whichever is nearer.
func nearestOf(m *Mob, current, other world.Entity) world.Entity {
	if current == nil || m.DistanceToSqr(other) <= m.DistanceToSqr(current) {
		return other
	}
	return current
}

// hoglinSetAvoidTarget ports HoglinAi.setAvoidTarget: the hoglin runs from
// the target for five to twenty seconds.
func hoglinSetAvoidTarget(m *Mob, avoid world.Entity) {
	b := m.brain()
	b.erase(memAttackTarget)
	b.erase(memWalkTarget)
	b.setWithExpiry(memAvoidTarget, avoid.H(), int64(100+m.Random().NextInt(301)))
}

// hoglinSetAttackTarget ports HoglinAi.setAttackTarget.
func hoglinSetAttackTarget(m *Mob, target world.Entity) {
	b := m.brain()
	b.erase(memCantReachWalkTargetSince)
	b.erase(memBreedTarget)
	b.setWithExpiry(memAttackTarget, target.H(), 200)
}

// hoglinBroadcastAttackTarget ports HoglinAi.broadcastAttackTarget: each
// adult hoglin in sight that is not pacified fights the nearer of its
// target and this one.
func hoglinBroadcastAttackTarget(m *Mob, target world.Entity) {
	for _, h := range visibleAdultHoglins(m) {
		if h.brain().has(memPacified) {
			continue
		}
		hoglinSetAttackTarget(h, nearestOf(h, h.brain().entity(h, memAttackTarget), target))
	}
}

// hoglinWasHurtBy ports HoglinAi.wasHurtBy: a hurt hoglin is no longer
// pacified or in love, a piglet runs, and an adult fights back.
func hoglinWasHurtBy(m *Mob, s *netherBeastState, attacker world.Entity) {
	b := m.brain()
	b.erase(memPacified)
	b.erase(memBreedTarget)
	if s.age < 0 {
		hoglinRetreatFrom(m, attacker)
		return
	}
	if b.isActive(activityAvoid) && attacker.H().Type().EncodeEntity() == "minecraft:piglin" || attacker.H().Type() == hoglinType {
		return
	}
	if otherTargetMuchFurther(m, attacker, 4) || !sensorAttackable(m, attacker) {
		return
	}
	hoglinSetAttackTarget(m, attacker)
	hoglinBroadcastAttackTarget(m, attacker)
}

// otherTargetMuchFurther ports
// BehaviorUtils.isOtherTargetMuchFurtherAwayThanCurrentAttackTarget.
func otherTargetMuchFurther(m *Mob, other world.Entity, by float64) bool {
	current := m.brain().entity(m, memAttackTarget)
	if current == nil {
		return false
	}
	return m.DistanceToSqr(other) > m.DistanceToSqr(current)+by*by
}

func (h *hoglin) Health() float64        { return h.beast().health.Health() }
func (z *zoglin) Health() float64        { return z.beast().health.Health() }
func (h *hoglin) MaxHealth() float64     { return h.beast().health.MaxHealth() }
func (z *zoglin) MaxHealth() float64     { return z.beast().health.MaxHealth() }
func (h *hoglin) SetMaxHealth(v float64) { h.beast().health.SetMaxHealth(v) }
func (z *zoglin) SetMaxHealth(v float64) { z.beast().health.SetMaxHealth(v) }
func (h *hoglin) Dead() bool             { return h.beast().dead || h.Health() <= 0 }
func (z *zoglin) Dead() bool             { return z.beast().dead || z.Health() <= 0 }
func beastHurt(e *Ent, self world.Entity, b *netherBeastState, d float64, src world.DamageSource) (float64, bool) {
	if d <= 0 || b.dead {
		return 0, false
	}
	var ok bool
	if d, ok = b.ai.DamageCooldown(e.tx, d); !ok {
		return 0, false
	}
	if d, ok = mobEffectDamage(b.effects, d, src); !ok {
		return 0, false
	}
	b.health.AddHealth(-d)
	b.ai.Hurt(src)
	if a, ok := self.(breeder); ok {
		// Animal.hurtServer ends love mode.
		resetLove(a)
	}
	beastViewAction(e, HurtAction{})
	onMobHurt(e.tx, self)
	if b.health.Health() <= 0 {
		b.dead = true
		beastViewAction(e, DeathAction{})
		dropAllDeathLoot(e.tx, self, src)
		_ = e.CloseIn(e.tx)
		return d, true
	}
	if attacker, ok := mobKiller(src).(Living); ok && attacker != nil {
		m := b.ai.Mob(e, self)
		if b.kind == hoglinKind {
			hoglinWasHurtBy(m, b, attacker)
		} else if m.CanAttack(attacker) && !otherTargetMuchFurther(m, attacker, 4) {
			// Zoglin.hurtServer.
			m.brain().erase(memCantReachWalkTargetSince)
			m.brain().setWithExpiry(memAttackTarget, attacker.H(), 200)
		}
	}
	return d, true
}
func (h *hoglin) viewAction(a world.EntityAction) { beastViewAction(h.Ent, a) }
func (z *zoglin) viewAction(a world.EntityAction) { beastViewAction(z.Ent, a) }
func beastViewAction(e *Ent, a world.EntityAction) {
	mob, ok := e.handle.Entity(e.tx)
	if !ok {
		return
	}
	world.BroadcastEntityAction(e.tx, mob, a)
}
func (h *hoglin) Heal(v float64, _ world.HealingSource) float64 { return beastHeal(h.beast(), v) }
func (z *zoglin) Heal(v float64, _ world.HealingSource) float64 { return beastHeal(z.beast(), v) }
func beastHeal(b *netherBeastState, v float64) float64 {
	if v <= 0 {
		return 0
	}
	old := b.health.Health()
	b.health.AddHealth(v)
	return b.health.Health() - old
}
func (h *hoglin) KnockBack(p mgl64.Vec3, f, k float64) { knockBack(h, p, f, k) }
func (z *zoglin) KnockBack(p mgl64.Vec3, f, k float64) { knockBack(z, p, f, k) }
func (h *hoglin) Hurt(d float64, src world.DamageSource) (float64, bool) {
	return beastHurt(h.Ent, h, h.beast(), d, src)
}
func (z *zoglin) Hurt(d float64, src world.DamageSource) (float64, bool) {
	return beastHurt(z.Ent, z, z.beast(), d, src)
}
func (h *hoglin) AddEffect(e effect.Effect)  { h.beast().effects.Add(e, h) }
func (z *zoglin) AddEffect(e effect.Effect)  { z.beast().effects.Add(e, z) }
func (h *hoglin) RemoveEffect(t effect.Type) { h.beast().effects.Remove(t, h) }
func (z *zoglin) RemoveEffect(t effect.Type) { z.beast().effects.Remove(t, z) }
func (h *hoglin) Effects() []effect.Effect   { return h.beast().effects.Effects() }
func (z *zoglin) Effects() []effect.Effect   { return z.beast().effects.Effects() }
func (h *hoglin) Speed() float64             { return h.beast().speed }
func (z *zoglin) Speed() float64             { return z.beast().speed }
func (h *hoglin) SetSpeed(v float64)         { h.beast().speed = v }
func (z *zoglin) SetSpeed(v float64)         { z.beast().speed = v }
func (h *hoglin) FinalizeSpawn(reason string, r *vanilla.Random) {
	if r == nil {
		return
	}
	if reason != "conversion" {
		h.beast().age = 0
		if r.NextFloat() < .2 {
			h.beast().age = -24000
		}
	}
	h.beast().immune = false
}
func (z *zoglin) FinalizeSpawn(_ string, _ *vanilla.Random) {}

// InLove reports whether the hoglin is in love.
func (h *hoglin) InLove() bool { return h.beast().breeding.InLove() }

func (h *hoglin) breedingState() *breeding { return &h.beast().breeding }

// ageTicks returns the age of the hoglin, negative while it is a piglet.
func (h *hoglin) ageTicks() *int { return &h.beast().age }

// breedingFood reports whether the stack is in #hoglin_food.
func (h *hoglin) breedingFood(stack item.Stack) bool {
	return blockItemInTag(stack, "hoglin_food") || inItemTag(stack, "hoglin_food")
}

// canFallInLove ports Hoglin.canFallInLove: a hoglin pacified by a
// repellent will not fall in love.
func (h *hoglin) canFallInLove() bool { return !h.beast().br.has(memPacified) }

// CanBeHunted ports Hoglin.canBeHunted: piglins hunt adult hoglins that were
// not spawned as part of a piglin's hunt.
func (h *hoglin) CanBeHunted() bool { b := h.beast(); return b.age >= 0 && !b.cannotBeHunted }

func (h *hoglin) breedOffspring(*world.Tx, breeder) *world.EntityHandle {
	return babyAt(h).New(hoglinType, netherBeastConfig{kind: hoglinKind, age: babyStartAge})
}

// Interact ports Animal.mobInteract for hoglins: crimson fungus feeds them.
func (h *hoglin) Interact(_ *world.Tx, u item.User, c *item.UseContext) bool {
	return u != nil && feedBreeder(h, u, c)
}
func (h *hoglin) Age() int { return h.beast().age }
func (z *zoglin) Age() int { return z.beast().age }

var _ Interactable = (*hoglin)(nil)
var hoglinType = netherBeastType{id: "minecraft:hoglin", kind: hoglinKind}
var zoglinType = netherBeastType{id: "minecraft:zoglin", kind: zoglinKind}

type netherBeastType struct {
	id   string
	kind netherBeastKind
}

func NewHoglin(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(hoglinType, netherBeastConfig{kind: hoglinKind})
}
func NewZoglin(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(zoglinType, netherBeastConfig{kind: zoglinKind})
}

var _ world.EntityType = netherBeastType{}
var _ Living = (*hoglin)(nil)
var _ Living = (*zoglin)(nil)

func (b *netherBeastState) effectManager() *EffectManager { return b.effects }
