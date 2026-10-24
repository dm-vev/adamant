package entity

import (
	"math"
	"math/rand/v2"
	"slices"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// WardenType is the registered Java Warden type.
var WardenType wardenType

type wardenType struct{}

func (wardenType) EncodeEntity() string { return "minecraft:warden" }

// BBox ports Warden.getDefaultDimensions: a warden digging or emerging is a
// block tall.
func (wardenType) BBox(e world.Entity) cube.BBox {
	if w, ok := e.(*Warden); ok && w.state().diggingOrEmerging() {
		return cube.Box(-.45, 0, -.45, .45, 1, .45)
	}
	return cube.Box(-.45, 0, -.45, .45, 2.9, .45)
}
func (wardenType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	if d.Data == nil {
		wardenConfig{}.Apply(d)
	}
	return &Warden{Ent: &Ent{tx: tx, handle: h, data: d}}
}
func (wardenType) DecodeNBT(n map[string]any, d *world.EntityData) {
	c := wardenConfig{health: nativeBossHealth(n, 500)}
	if java := ReadNativeMobJavaState(n); java != nil {
		if anger, ok := java["anger"].(map[string]any); ok {
			suspects, _ := anger["suspects"].([]any)
			for _, raw := range suspects {
				s, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				id, err := uuid.Parse(javaUUIDString(s["uuid"]))
				if err == nil {
					c.anger = append(c.anger, wardenSavedAnger{id, int(nbtconv.Int32(s, "anger"))})
				}
			}
		}
	}
	c.Apply(d)
	d.Data.(*wardenState).speed = nativeBossMovementSpeed(n, wardenBaseMovementSpeed)
}
func (wardenType) EncodeNBT(d *world.EntityData) map[string]any {
	s := d.Data.(*wardenState)
	attrs := []NativeMobAttribute{
		{Name: "minecraft:health", Base: 500, Current: s.health.Health(), DefaultMin: 0, DefaultMax: 500, Min: 0, Max: 500},
		{Name: "minecraft:movement", Base: wardenBaseMovementSpeed, Current: s.speed, DefaultMin: 0, DefaultMax: 1024, Min: 0, Max: 1024},
		{Name: "minecraft:follow_range", Base: 24, Current: 24, DefaultMin: 0, DefaultMax: 2048, Min: 0, Max: 2048},
		{Name: "minecraft:attack_damage", Base: 30, Current: 30, DefaultMin: 0, DefaultMax: 2048, Min: 0, Max: 2048},
	}
	var suspects []any
	for _, a := range s.anger.saved() {
		suspects = append(suspects, map[string]any{"uuid": javaUUIDIntArray(a.id.String()), "anger": int32(a.anger)})
	}
	return encodeBossNBT(d, attrs, map[string]any{"anger": map[string]any{"suspects": suspects}})
}

type wardenSavedAnger struct {
	id    uuid.UUID
	anger int
}

type wardenConfig struct {
	health float64
	anger  []wardenSavedAnger
}

func (c wardenConfig) Apply(d *world.EntityData) {
	h := c.health
	if h <= 0 {
		h = 500
	}
	s := &wardenState{health: NewHealthManager(h, 500), effects: NewEffectManager(), speed: wardenBaseMovementSpeed, vibrations: &world.VibrationData{}}
	s.anger = newAngerManagement(c.anger)
	s.br = newWardenBrain(s)
	d.Data = s
}

// wardenPose is the pose of a warden, from the Pose of Java.
type wardenPose uint8

const (
	wardenStanding wardenPose = iota
	wardenEmerging
	wardenDigging
	wardenRoaring
	wardenSniffing
)

type wardenState struct {
	health     *HealthManager
	effects    *EffectManager
	ai         MobAI
	br         *Brain
	speed      float64
	dead       bool
	pose       wardenPose
	sonicBoom  bool
	anger      *angerManagement
	vibrations *world.VibrationData
	// clientAnger is Warden.CLIENT_ANGER_LEVEL, the anger viewers see.
	clientAnger int
}

// Warden is the blind guardian of the deep dark, which hunts by sound and
// smell.
type Warden struct{ *Ent }

func NewWarden(o world.EntitySpawnOpts) *world.EntityHandle { return o.New(WardenType, wardenConfig{}) }
func (*Warden) SpeciesType() world.EntityType               { return WardenType }
func (w *Warden) state() *wardenState                       { return w.Behaviour().(*wardenState) }

// Tick ports Warden.tick: the vibrations a warden hears travel to it.
func (w *Warden) Tick(tx *world.Tx, n int64) {
	s := w.state()
	s.effects.Tick(w, tx)
	w.Ent.Tick(tx, n)
	if !s.dead && w.tx != nil {
		tx.World().TickEntityVibrationSystem(tx, w)
	}
}

// FinalizeSpawn ports Warden.finalizeSpawn: a warden may not dig away for a
// minute, and one a shrieker summons emerges from the ground.
func (w *Warden) FinalizeSpawn(reason string, _ *vanilla.Random) {
	s := w.state()
	s.br.setWithExpiry(memDigCooldown, true, 1200)
	if reason == "triggered" {
		s.pose = wardenEmerging
		s.br.setWithExpiry(memIsEmerging, true, wardenEmergeTicks)
		w.tx.PlaySound(w.Position(), sound.NativeEvent{SoundType: "agitated", EntityType: "minecraft:warden", ExtraData: -1})
	}
}

func (s *wardenState) mobAI() *MobAI { return &s.ai }
func (s *wardenState) brain() *Brain { return s.br }

// The durations of the activities of a warden, from WardenAi.
const (
	wardenDiggingTicks  = 100
	wardenEmergeTicks   = 134
	wardenRoarTicks     = 84
	wardenSniffingTicks = 84
)

// Tick runs the MobAI of the warden with its brain.
func (s *wardenState) Tick(e *Ent, tx *world.Tx) *Movement {
	// Warden.createAttributes.
	s.ai.MovementSpeed, s.ai.FollowRange, s.ai.EyeHeight = s.speed, 24, float64(float32(2.9)*.85)
	return s.ai.Tick(e, &Warden{Ent: e}, func(m *Mob) {
		m.AI.nav.SetCanFloat(true)
		m.AI.SetPathfindingMalus(PathUnpassableRail, 0)
		m.AI.SetPathfindingMalus(PathDamaging, 8)
		m.AI.SetPathfindingMalus(PathPowderSnow, 8)
		m.AI.SetPathfindingMalus(PathLava, 8)
		m.AI.SetPathfindingMalus(PathFire, 0)
		m.AI.SetPathfindingMalus(PathFireInNeighbour, 0)
	})
}

// customServerAiStep ports Warden.customServerAiStep: a warden darkens the
// sight of players around it every six seconds, and its anger cools down.
func (s *wardenState) customServerAiStep(m *Mob) {
	s.br.tick(m)
	if (m.AI.tickCount+int(m.H().UniqueID()&0x7fffffff))%120 == 0 {
		applyDarknessAround(m.tx, m.data.Pos, 20)
	}
	if m.AI.tickCount%20 == 0 {
		s.anger.tick(m.tx, func(e world.Entity) bool { return wardenCanTarget(m, e) })
		s.syncClientAnger(m)
	}
	s.br.setActiveToFirstValid(activityEmerge, activityDig, activityRoar, activityFight, activityInvestigate, activitySniff, activityIdle)
}

// afterAiStep ports Warden.doPush for the entities the warden touches: it
// grows angry at them and turns to where they are.
func (s *wardenState) afterAiStep(m *Mob) {
	if m.AI.NoAI || s.dead || s.diggingOrEmerging() {
		return
	}
	box := m.BBox()
	for e := range m.tx.EntitiesWithin(box.Grow(.5)) {
		if e.H() == m.H() || spectator(e) || !entityAlive(e) || !e.H().Type().BBox(e).Translate(e.Position()).IntersectsWith(box) {
			continue
		}
		if s.br.has(memTouchCooldown) {
			return
		}
		s.br.setWithExpiry(memTouchCooldown, true, 20)
		s.increaseAngerAt(m, e, 35, true)
		wardenSetDisturbanceLocation(m, s, cube.PosFromVec3(e.Position()))
	}
}

// applyDarknessAround ports Warden.applyDarknessAround: players in survival
// or adventure within the radius are blinded by darkness for thirteen
// seconds unless they already are for longer than ten.
func applyDarknessAround(tx *world.Tx, pos mgl64.Vec3, radius float64) {
	for p := range tx.Players() {
		g, ok := p.(interface{ GameMode() world.GameMode })
		if !ok || (g.GameMode() != world.GameModeSurvival && g.GameMode() != world.GameModeAdventure) {
			continue
		}
		if p.Position().Sub(pos).LenSqr() >= radius*radius {
			continue
		}
		l, ok := p.(interface {
			Living
			Effect(effect.Type) (effect.Effect, bool)
		})
		if !ok {
			continue
		}
		if current, has := l.Effect(effect.Darkness); has && current.Duration() > 199*time.Second/20 {
			continue
		}
		l.AddEffect(effect.New(effect.Darkness, 1, 260*time.Second/20).WithoutParticles())
	}
}

// diggingOrEmerging ports Warden.isDiggingOrEmerging.
func (s *wardenState) diggingOrEmerging() bool {
	return s.pose == wardenDigging || s.pose == wardenEmerging
}

func (s *wardenState) setPose(m *Mob, pose wardenPose) {
	if s.pose != pose {
		s.pose = pose
		m.updateState()
	}
}

// activeAnger ports Warden.getActiveAnger: the anger at the target of the
// warden, or the highest anger without one.
func (s *wardenState) activeAnger(m *Mob) int {
	return s.anger.activeAnger(s.br.entity(m, memAttackTarget))
}

// syncClientAnger ports Warden.syncClientAngerLevel: viewers hear the heart
// of the warden beat faster the angrier it is.
func (s *wardenState) syncClientAnger(m *Mob) {
	if a := s.activeAnger(m); a != s.clientAnger {
		s.clientAnger = a
		m.updateState()
	}
}

// wardenAngry ports AngerLevel.isAngry: anger of 80 or more.
func wardenAngry(anger int) bool { return anger >= 80 }

// entityAngryAt ports Warden.getEntityAngryAt: the suspect the warden is
// angriest at, while it is angry.
func (s *wardenState) entityAngryAt(m *Mob) world.Entity {
	if !wardenAngry(s.activeAnger(m)) {
		return nil
	}
	return s.anger.activeEntity(m.tx, func(e world.Entity) bool { return wardenCanTarget(m, e) })
}

// increaseAngerAt ports Warden.increaseAngerAt: a warden grows angry at an
// entity it may target, switching from a target that is no player to an
// angering player.
func (s *wardenState) increaseAngerAt(m *Mob, e world.Entity, amount int, playSound bool) {
	if m.AI.NoAI || !wardenCanTarget(m, e) {
		return
	}
	wardenSetDigCooldown(s.br)
	target := s.br.entity(m, memAttackTarget)
	maybeSwitch := target == nil || !isPlayerEntity(target)
	anger := s.anger.increaseAnger(e, amount)
	if isPlayerEntity(e) && maybeSwitch && wardenAngry(anger) {
		s.br.erase(memAttackTarget)
	}
	if playSound && s.pose != wardenRoaring {
		// Warden.playListeningSound.
		name := "listening"
		if s.activeAnger(m) >= 40 {
			name = "listening_angry"
		}
		m.tx.PlaySound(m.data.Pos, sound.NativeEvent{SoundType: name, EntityType: "minecraft:warden", ExtraData: -1})
	}
}

// setAttackTarget ports Warden.setAttackTarget.
func (s *wardenState) setAttackTarget(target world.Entity) {
	s.br.erase(memRoarTarget)
	s.br.setEntity(memAttackTarget, target)
	s.br.erase(memCantReachWalkTargetSince)
	s.br.setWithExpiry(memSonicBoomCooldown, true, 200)
}

// wardenCanTarget ports Warden.canTargetEntity: a living entity alive in the
// world that is no warden or armour stand and no player in creative or
// spectator mode.
func wardenCanTarget(m *Mob, e world.Entity) bool {
	if e == nil {
		return false
	}
	if _, living := e.(Living); !living || !entityAlive(e) || spectator(e) {
		return false
	}
	if g, ok := e.(interface{ GameMode() world.GameMode }); ok && g.GameMode().CreativeInventory() {
		return false
	}
	switch e.H().Type().EncodeEntity() {
	case "minecraft:warden", "minecraft:armor_stand":
		return false
	}
	_, ok := m.tx.EntityByUUID(e.H().UUID())
	return ok
}

// canAttackEntity ports Warden.canAttack.
func (s *wardenState) canAttackEntity(m *Mob, t world.Entity) bool { return wardenCanTarget(m, t) }

// walkTargetValue ports Warden.getWalkTargetValue.
func (s *wardenState) walkTargetValue(*Mob, cube.Pos) float64 { return 0 }

// doHurtTarget ports Warden.doHurtTarget: a warden hits for 30 damage and
// knocks its target back hard, and may not use its sonic boom for two
// seconds after.
func (s *wardenState) doHurtTarget(m *Mob, target world.Entity) bool {
	l, ok := target.(Living)
	if !ok {
		return false
	}
	w := m.Self.(*Warden)
	w.viewAction(SwingArmAction{})
	m.tx.PlaySound(m.data.Pos, sound.NativeEvent{SoundType: "attack", EntityType: "minecraft:warden", ExtraData: -1})
	s.br.setWithExpiry(memSonicBoomCooldown, true, 40)
	if _, hurt := l.Hurt(30, AttackDamageSource{Attacker: w}); !hurt {
		return false
	}
	// Mob.causeExtraKnockback with an attack knockback of 1.5.
	yaw := m.Yaw() * math.Pi / 180
	l.KnockBack(target.Position().Sub(mgl64.Vec3{math.Sin(yaw), 0, -math.Cos(yaw)}), 1.5*.5, .4)
	m.data.Vel[0], m.data.Vel[2] = m.data.Vel[0]*.6, m.data.Vel[2]*.6
	return true
}

// newWardenBrain ports WardenAi and the brain provider of Warden.
func newWardenBrain(s *wardenState) *Brain {
	br := newBrain(playerSensor{}, wardenEntitySensor{})
	for _, mem := range []memoryType{memNearestVisibleNemesis, memRecentProjectile, memTouchCooldown, memVibrationCooldown, memDigCooldown} {
		br.register(mem)
	}
	angryAt := func(m *Mob) world.Entity { return s.entityAngryAt(m) }
	br.addActivity(activityCore, 0, newSwim(.8), newSetWardenLookTarget(), newLookAtTargetSink(45, 90), newMoveToTargetSink(150, 250, nil))
	br.addActivityWith(activityEmerge, []prioritizedBehavior{{5, newWardenEmerging(s)}}, []memoryCondition{{memIsEmerging, memPresent}}, nil)
	br.addActivityWith(activityDig, []prioritizedBehavior{{0, newForceUnmount()}, {1, newWardenDigging(s)}},
		[]memoryCondition{{memRoarTarget, memAbsent}, {memDigCooldown, memAbsent}}, nil)
	br.addActivity(activityIdle, 10,
		newSetRoarTarget(angryAt),
		newTryToSniff(s),
		newRunOne([]memoryCondition{{memIsSniffing, memAbsent}},
			weightedBehavior{newRandomStroll(.5, true), 2},
			weightedBehavior{newDoNothing(30, 60), 1},
		),
	)
	br.addActivityWith(activityRoar, []prioritizedBehavior{{10, newWardenRoar(s)}}, []memoryCondition{{memRoarTarget, memPresent}}, nil)
	br.addActivityWith(activityFight, []prioritizedBehavior{
		{10, newOneShot([]memoryCondition{{memDigCooldown, memRegistered}}, func(_ *Mob, b *Brain, _ int64) bool {
			wardenSetDigCooldown(b)
			return true
		})},
		{10, newStopAttackingIfTargetInvalid(func(m *Mob, t world.Entity) bool {
			return !wardenAngry(s.activeAnger(m)) || !wardenCanTarget(m, t)
		}, func(m *Mob, t world.Entity) {
			// WardenAi.onTargetInvalid.
			if !wardenCanTarget(m, t) {
				s.anger.clearAnger(t)
			}
			wardenSetDigCooldown(s.br)
		}, false)},
		{10, newSetEntityLookTarget(nil, 24)},
		{10, newSetWalkTargetFromAttackTargetIfTargetOutOfReach(constSpeed(float64(float32(1.2))))},
		{10, newWardenSonicBoom(s)},
		{10, newMeleeAttack(18)},
	}, []memoryCondition{{memAttackTarget, memPresent}}, nil)
	br.addActivityWith(activityInvestigate, []prioritizedBehavior{
		{5, newSetRoarTarget(angryAt)},
		{5, newGoToTargetLocation(memDisturbanceLocation, 2, float64(float32(.7)))},
	}, []memoryCondition{{memDisturbanceLocation, memPresent}}, nil)
	br.addActivityWith(activitySniff, []prioritizedBehavior{
		{5, newSetRoarTarget(angryAt)},
		{5, newWardenSniffing(s)},
	}, []memoryCondition{{memIsSniffing, memPresent}}, nil)
	return br
}

// wardenSetDigCooldown ports WardenAi.setDigCooldown: a warden on its dig
// cooldown may not dig away for another minute.
func wardenSetDigCooldown(b *Brain) {
	if b.has(memDigCooldown) {
		b.setWithExpiry(memDigCooldown, true, 1200)
	}
}

// wardenSetDisturbanceLocation ports WardenAi.setDisturbanceLocation: a calm
// warden without a target goes to look at where something disturbed it.
func wardenSetDisturbanceLocation(m *Mob, s *wardenState, pos cube.Pos) {
	if s.entityAngryAt(m) != nil || s.br.has(memAttackTarget) {
		return
	}
	wardenSetDigCooldown(s.br)
	s.br.setWithExpiry(memSniffCooldown, true, 100)
	s.br.setWithExpiry(memLookTarget, newBlockPosTracker(pos), 100)
	s.br.setWithExpiry(memDisturbanceLocation, pos, 100)
	s.br.erase(memWalkTarget)
}

// wardenEntitySensor ports WardenEntitySensor: the nearest player the warden
// may target, or else the nearest other entity it may.
type wardenEntitySensor struct{ nearestLivingEntitySensor }

func (s wardenEntitySensor) requires() []memoryType {
	return append(s.nearestLivingEntitySensor.requires(), memNearestAttackable)
}

func (s wardenEntitySensor) doTick(m *Mob, b *Brain) {
	s.nearestLivingEntitySensor.doTick(m, b)
	near := nearestLiving(m)
	for _, players := range [...]bool{true, false} {
		for _, e := range near {
			if isPlayerEntity(e) == players && wardenCanTarget(m, e) {
				b.setEntity(memNearestAttackable, e)
				return
			}
		}
	}
	b.erase(memNearestAttackable)
}

// newSetWardenLookTarget ports SetWardenLookTarget: a warden without a
// target looks at what it roars at or what disturbed it.
func newSetWardenLookTarget() *oneShot {
	return newOneShot([]memoryCondition{{memLookTarget, memRegistered}, {memDisturbanceLocation, memRegistered}, {memRoarTarget, memRegistered}, {memAttackTarget, memAbsent}}, func(m *Mob, b *Brain, _ int64) bool {
		var pos cube.Pos
		if roar := b.entity(m, memRoarTarget); roar != nil {
			pos = cube.PosFromVec3(roar.Position())
		} else if v, ok := b.get(memDisturbanceLocation); ok {
			pos = v.(cube.Pos)
		} else {
			return false
		}
		b.set(memLookTarget, newBlockPosTracker(pos))
		return true
	})
}

// newSetRoarTarget ports SetRoarTarget: a warden roars at what it is angry at.
func newSetRoarTarget(finder func(m *Mob) world.Entity) *oneShot {
	return newOneShot([]memoryCondition{{memRoarTarget, memAbsent}, {memAttackTarget, memAbsent}, {memCantReachWalkTargetSince, memRegistered}}, func(m *Mob, b *Brain, _ int64) bool {
		target := finder(m)
		if !wardenCanTarget(m, target) {
			return false
		}
		b.setEntity(memRoarTarget, target)
		b.erase(memCantReachWalkTargetSince)
		return true
	})
}

// newTryToSniff ports TryToSniff: a warden with something it may target near
// sniffs for it now and then.
func newTryToSniff(s *wardenState) *oneShot {
	return newOneShot([]memoryCondition{{memIsSniffing, memRegistered}, {memWalkTarget, memRegistered}, {memSniffCooldown, memAbsent},
		{memNearestAttackable, memPresent}, {memDisturbanceLocation, memAbsent}}, func(m *Mob, b *Brain, _ int64) bool {
		b.set(memIsSniffing, true)
		b.setWithExpiry(memSniffCooldown, true, int64(uniformInt{100, 200}.sample(m)))
		b.erase(memWalkTarget)
		s.setPose(m, wardenSniffing)
		return true
	})
}

// newForceUnmount ports ForceUnmount: a warden about to dig gets off what it
// rides.
func newForceUnmount() *behavior {
	return &behavior{
		checkExtra: func(m *Mob) bool { return m.data.Riding != nil },
		start: func(m *Mob, _ int64) {
			if v, ok := m.data.Riding.Entity(m.tx); ok {
				if d, ok := v.(PassengerDismounter); ok {
					d.Dismount(m.tx, m.Self)
				}
			}
		},
		canStillUse: func(*Mob, int64) bool { return false },
	}
}

// newWardenEmerging ports Emerging: a warden rises from the ground.
func newWardenEmerging(s *wardenState) *behavior {
	return &behavior{
		entry:       []memoryCondition{{memIsEmerging, memPresent}, {memWalkTarget, memAbsent}, {memLookTarget, memRegistered}},
		minDuration: wardenEmergeTicks, maxDuration: wardenEmergeTicks,
		canStillUse: func(*Mob, int64) bool { return true },
		start:       func(m *Mob, _ int64) { s.setPose(m, wardenEmerging) },
		stop: func(m *Mob, _ int64) {
			if s.pose == wardenEmerging {
				s.setPose(m, wardenStanding)
			}
		},
	}
}

// newWardenDigging ports Digging: a warden with nothing to do for a minute
// digs back into the ground and is gone.
func newWardenDigging(s *wardenState) *behavior {
	bh := &behavior{
		entry:       []memoryCondition{{memAttackTarget, memAbsent}, {memWalkTarget, memAbsent}},
		minDuration: wardenDiggingTicks, maxDuration: wardenDiggingTicks,
		checkExtra:  func(m *Mob) bool { return m.OnGround() || m.InLiquid() },
		canStillUse: func(m *Mob, _ int64) bool { return !s.dead },
	}
	bh.stop = func(m *Mob, _ int64) {
		if !s.dead {
			s.dead = true
			_ = m.CloseIn(m.tx)
		}
	}
	bh.start = func(m *Mob, time int64) {
		if m.OnGround() {
			s.setPose(m, wardenDigging)
			return
		}
		m.tx.PlaySound(m.data.Pos, sound.NativeEvent{SoundType: "agitated", EntityType: "minecraft:warden", ExtraData: -1})
		bh.stop(m, time)
	}
	return bh
}

// newWardenRoar ports Roar: a warden roars at what angers it for four
// seconds, then attacks it.
func newWardenRoar(s *wardenState) *behavior {
	return &behavior{
		entry:       []memoryCondition{{memRoarTarget, memPresent}, {memAttackTarget, memAbsent}, {memRoarSoundCooldown, memRegistered}, {memRoarSoundDelay, memRegistered}},
		minDuration: wardenRoarTicks, maxDuration: wardenRoarTicks,
		canStillUse: func(*Mob, int64) bool { return true },
		start: func(m *Mob, _ int64) {
			b := m.Brain()
			b.setWithExpiry(memRoarSoundDelay, true, 25)
			b.erase(memWalkTarget)
			target := b.entity(m, memRoarTarget)
			if target != nil {
				// BehaviorUtils.lookAtEntity.
				b.set(memLookTarget, newEntityTracker(target, true))
			}
			s.setPose(m, wardenRoaring)
			s.increaseAngerAt(m, target, 20, false)
		},
		tick: func(m *Mob, _ int64) {
			b := m.Brain()
			if !b.has(memRoarSoundDelay) && !b.has(memRoarSoundCooldown) {
				b.setWithExpiry(memRoarSoundCooldown, true, wardenRoarTicks-25)
			}
		},
		stop: func(m *Mob, _ int64) {
			if s.pose == wardenRoaring {
				s.setPose(m, wardenStanding)
			}
			b := m.Brain()
			if target := b.entity(m, memRoarTarget); target != nil {
				s.setAttackTarget(target)
			}
			b.erase(memRoarTarget)
		},
	}
}

// newWardenSniffing ports Sniffing: a warden sniffs for four seconds, then
// grows angry at what it may target close by and goes to where it is.
func newWardenSniffing(s *wardenState) *behavior {
	return &behavior{
		entry: []memoryCondition{{memIsSniffing, memPresent}, {memAttackTarget, memAbsent}, {memWalkTarget, memAbsent}, {memLookTarget, memRegistered},
			{memNearestAttackable, memRegistered}, {memDisturbanceLocation, memRegistered}, {memSniffCooldown, memRegistered}},
		minDuration: wardenSniffingTicks, maxDuration: wardenSniffingTicks,
		canStillUse: func(*Mob, int64) bool { return true },
		stop: func(m *Mob, _ int64) {
			if s.pose == wardenSniffing {
				s.setPose(m, wardenStanding)
			}
			b := m.Brain()
			b.erase(memIsSniffing)
			e := b.entity(m, memNearestAttackable)
			if !wardenCanTarget(m, e) {
				return
			}
			if wardenCloserThan(m, e, 6, 20) {
				s.increaseAngerAt(m, e, 35, true)
			}
			if !b.has(memDisturbanceLocation) {
				wardenSetDisturbanceLocation(m, s, cube.PosFromVec3(e.Position()))
			}
		},
	}
}

// wardenCloserThan ports Entity.closerThan with separate horizontal and
// vertical distances.
func wardenCloserThan(m *Mob, e world.Entity, horizontal, vertical float64) bool {
	d := e.Position().Sub(m.data.Pos)
	return d[0]*d[0]+d[2]*d[2] < horizontal*horizontal && math.Abs(d[1]) < vertical
}

// newWardenSonicBoom ports SonicBoom: a warden charges for 1.7 seconds and
// blasts its target within fifteen blocks through any block or armour.
func newWardenSonicBoom(s *wardenState) *behavior {
	const duration, soundTicks = 60, 34
	return &behavior{
		entry: []memoryCondition{{memAttackTarget, memPresent}, {memSonicBoomCooldown, memAbsent}, {memSonicBoomSoundCooldown, memRegistered},
			{memSonicBoomSoundDelay, memRegistered}},
		minDuration: duration, maxDuration: duration,
		checkExtra: func(m *Mob) bool {
			target := m.Brain().entity(m, memAttackTarget)
			return target != nil && wardenCloserThan(m, target, 15, 20)
		},
		canStillUse: func(*Mob, int64) bool { return true },
		start: func(m *Mob, _ int64) {
			b := m.Brain()
			b.setWithExpiry(memAttackCoolingDown, true, duration)
			b.setWithExpiry(memSonicBoomSoundDelay, true, soundTicks)
			s.sonicBoom = true
			m.updateState()
			m.tx.PlaySound(m.data.Pos, sound.NativeEvent{SoundType: "sonic_charge", EntityType: "minecraft:warden", ExtraData: -1})
		},
		tick: func(m *Mob, _ int64) {
			b := m.Brain()
			target := b.entity(m, memAttackTarget)
			if target != nil {
				tp := target.Position()
				m.AI.look.SetLookAt(m, tp[0], tp[1], tp[2])
			}
			if b.has(memSonicBoomSoundDelay) || b.has(memSonicBoomSoundCooldown) {
				return
			}
			b.setWithExpiry(memSonicBoomSoundCooldown, true, duration-soundTicks)
			if !wardenCanTarget(m, target) || !wardenCloserThan(m, target, 15, 20) {
				return
			}
			source := m.data.Pos.Add(mgl64.Vec3{0, 1.6})
			delta := mgl64.Vec3{target.Position()[0], entityEyeY(target), target.Position()[2]}.Sub(source)
			dir := javaNormalize(delta)
			for i := 1; i < int(math.Floor(delta.Len()))+7; i++ {
				m.tx.AddParticle(source.Add(dir.Mul(float64(i))), particle.SonicExplosion{})
			}
			m.tx.PlaySound(m.data.Pos, sound.NativeEvent{SoundType: "sonic_boom", EntityType: "minecraft:warden", ExtraData: -1})
			l, ok := target.(Living)
			if !ok {
				return
			}
			if _, hurt := l.Hurt(10, SonicBoomDamageSource{Attacker: m.Self}); hurt {
				resistance := entityKnockbackResistance(target)
				if v, ok := target.(interface {
					Velocity() mgl64.Vec3
					SetVelocity(mgl64.Vec3)
				}); ok {
					v.SetVelocity(v.Velocity().Add(mgl64.Vec3{dir[0] * 2.5 * (1 - resistance), dir[1] * .5 * (1 - resistance), dir[2] * 2.5 * (1 - resistance)}))
				}
			}
		},
		stop: func(m *Mob, _ int64) {
			m.Brain().setWithExpiry(memSonicBoomCooldown, true, 40)
			s.sonicBoom = false
			m.updateState()
		},
	}
}

// VibrationUser returns the hearing of the warden.
func (w *Warden) VibrationUser() world.VibrationUser { return wardenVibrationUser{w} }

// VibrationData returns the vibration listener data of the warden.
func (w *Warden) VibrationData() *world.VibrationData { return w.state().vibrations }

// VibrationListenerPosition returns the eyes of the warden, where its
// EntityPositionSource is.
func (w *Warden) VibrationListenerPosition() mgl64.Vec3 {
	return w.Position().Add(mgl64.Vec3{0, w.state().ai.EyeHeight})
}

// wardenVibrationUser ports Warden.VibrationUser: a warden hears vibrations
// within sixteen blocks and grows angry at what made them.
type wardenVibrationUser struct{ w *Warden }

func (wardenVibrationUser) VibrationRadius() int { return 16 }
func (wardenVibrationUser) ListensToGameEvent(e world.GameEvent) bool {
	return world.GameEventWardenCanListen(e)
}
func (u wardenVibrationUser) CanReceiveVibration(tx *world.Tx, _ cube.Pos, _ cube.Pos, _ world.GameEvent, ctx world.GameEventContext) bool {
	s := u.w.state()
	if s.ai.NoAI || s.dead || u.w.Dead() || s.br.has(memVibrationCooldown) || s.diggingOrEmerging() || !s.ai.started {
		return false
	}
	if _, living := ctx.Source.(Living); living {
		return wardenCanTarget(s.ai.Mob(u.w.Ent, u.w), ctx.Source)
	}
	return true
}
func (u wardenVibrationUser) ReceiveVibration(tx *world.Tx, _, eventPos cube.Pos, info world.VibrationInfo, _ float32) {
	w := u.w
	s := w.state()
	if w.Dead() {
		return
	}
	m := s.ai.Mob(w.Ent, w)
	s.br.setWithExpiry(memVibrationCooldown, true, 40)
	w.viewAction(VibrationDetectedAction{})
	var source, owner world.Entity
	if info.SourceID != uuid.Nil {
		source, _ = tx.EntityByUUID(info.SourceID)
	}
	if info.ProjectileOwnerID != uuid.Nil {
		owner, _ = tx.EntityByUUID(info.ProjectileOwnerID)
	}
	suspicious := eventPos
	if owner != nil {
		if owner.Position().Sub(m.data.Pos).LenSqr() < 30*30 {
			if s.br.has(memRecentProjectile) {
				if wardenCanTarget(m, owner) {
					suspicious = cube.PosFromVec3(owner.Position())
				}
				s.increaseAngerAt(m, owner, 35, true)
			} else {
				s.increaseAngerAt(m, owner, 10, true)
			}
		}
		s.br.setWithExpiry(memRecentProjectile, true, 100)
	} else {
		s.increaseAngerAt(m, source, 35, true)
	}
	if !wardenAngry(s.activeAnger(m)) {
		active := s.anger.activeEntity(tx, func(e world.Entity) bool { return wardenCanTarget(m, e) })
		if owner != nil || active == nil || source != nil && active.H() == source.H() {
			wardenSetDisturbanceLocation(m, s, suspicious)
		}
	}
}
func (u wardenVibrationUser) VibrationParticle(destination mgl64.Vec3, travelTicks int) world.Particle {
	return particle.VibrationSignal{Destination: destination, TravelTicks: travelTicks}
}

// WardenPose reports the poses viewers see the warden in.
func (w *Warden) WardenPose() (emerging, digging, roaring, sniffing, sonicBoom bool) {
	s := w.state()
	return s.pose == wardenEmerging, s.pose == wardenDigging, s.pose == wardenRoaring, s.pose == wardenSniffing, s.sonicBoom
}

// HeartbeatInterval ports Warden.getHeartBeatDelay: the heart of a warden
// beats every two seconds when calm and four times as often when angry.
func (w *Warden) HeartbeatInterval() int32 {
	anger := float32(w.state().clientAnger) / 80
	return int32(40 - math.Floor(float64(min(max(anger, 0), 1)*30)))
}

// AngerAt returns how angry the warden is at an entity.
func (w *Warden) AngerAt(e world.Entity) int { return w.state().anger.angerOf(e) }

func (w *Warden) Health() float64        { return w.state().health.Health() }
func (w *Warden) MaxHealth() float64     { return w.state().health.MaxHealth() }
func (w *Warden) SetMaxHealth(v float64) { w.state().health.SetMaxHealth(v) }
func (w *Warden) Dead() bool             { return w.state().dead || w.Health() <= 0 }

// Hurt ports Warden.isInvulnerableTo and hurtServer: a warden digging or
// emerging takes no damage, and one hurt grows angry at its attacker and
// fights it if it hit directly or from close by.
func (w *Warden) Hurt(d float64, src world.DamageSource) (float64, bool) {
	s := w.state()
	if w.Dead() || d <= 0 {
		return 0, false
	}
	_, void := src.(VoidDamageSource)
	if s.diggingOrEmerging() && !void {
		return 0, false
	}
	var ok bool
	if d, ok = s.ai.DamageCooldown(w.tx, d); !ok {
		return 0, false
	}
	if d, ok = mobEffectDamage(s.effects, d, src); !ok {
		return 0, false
	}
	s.health.AddHealth(-d)
	s.ai.Hurt(src)
	w.viewAction(HurtAction{})
	onMobHurt(w.tx, w)
	if w.Dead() {
		s.dead = true
		w.viewAction(DeathAction{})
		dropAllDeathLoot(w.tx, w, src)
		_ = w.CloseIn(w.tx)
		return d, true
	}
	if !s.ai.NoAI && s.ai.started {
		m := s.ai.Mob(w.Ent, w)
		attacker := mobKiller(src)
		s.increaseAngerAt(m, attacker, 80+20, false)
		if !s.br.has(memAttackTarget) && attacker != nil {
			if _, living := attacker.(Living); living {
				_, indirect := src.(ProjectileDamageSource)
				if !indirect || attacker.Position().Sub(w.Position()).LenSqr() < 25 {
					s.setAttackTarget(attacker)
				}
			}
		}
	}
	return d, true
}
func (w *Warden) Heal(v float64, _ world.HealingSource) float64 {
	if v <= 0 || w.Dead() {
		return 0
	}
	a := w.Health()
	w.state().health.AddHealth(v)
	return w.Health() - a
}

// KnockBack does nothing: a warden resists all knockback.
func (w *Warden) KnockBack(mgl64.Vec3, float64, float64) {}
func (w *Warden) AddEffect(e effect.Effect) {
	if e.Type() != effect.Poison && e.Type() != effect.FatalPoison {
		w.state().effects.Add(e, w)
	}
}
func (w *Warden) RemoveEffect(t effect.Type) { w.state().effects.Remove(t, w) }
func (w *Warden) Effects() []effect.Effect   { return w.state().effects.Effects() }
func (w *Warden) Speed() float64             { return w.state().speed }
func (w *Warden) SetSpeed(v float64)         { w.state().speed = max(0, min(1024, v)) }
func (w *Warden) viewAction(a world.EntityAction) {
	v := w.tx.Viewers(w.Position())
	for _, x := range v {
		x.ViewEntityAction(w, a)
	}
	w.tx.ReleaseViewers(v)
}

// angerManagement ports AngerManagement: how angry a warden is at each
// suspect, angriest first with players before others, cooling down a point
// a second.
type angerManagement struct {
	conversionDelay int
	highestAnger    int
	suspects        []*angerSuspect
	// byUUID holds the anger at suspects not in the world, as read from NBT
	// or kept when they left.
	byUUID map[uuid.UUID]int
}

type angerSuspect struct {
	h     *world.EntityHandle
	anger int
}

func newAngerManagement(saved []wardenSavedAnger) *angerManagement {
	a := &angerManagement{conversionDelay: rand.IntN(3), byUUID: make(map[uuid.UUID]int, len(saved))}
	for _, s := range saved {
		a.byUUID[s.id] = s.anger
	}
	return a
}

// saved returns the anger to save, AngerManagement.createUuidAngerPairs.
func (a *angerManagement) saved() []wardenSavedAnger {
	var out []wardenSavedAnger
	for _, s := range a.suspects {
		out = append(out, wardenSavedAnger{s.h.UUID(), s.anger})
	}
	for id, anger := range a.byUUID {
		out = append(out, wardenSavedAnger{id, anger})
	}
	return out
}

func (a *angerManagement) find(e world.Entity) *angerSuspect {
	if e == nil {
		return nil
	}
	for _, s := range a.suspects {
		if s.h == e.H() {
			return s
		}
	}
	return nil
}

func (a *angerManagement) angerOf(e world.Entity) int {
	if s := a.find(e); s != nil {
		return s.anger
	}
	return 0
}

// tick ports AngerManagement.tick.
func (a *angerManagement) tick(tx *world.Tx, valid func(world.Entity) bool) {
	if a.conversionDelay--; a.conversionDelay <= 0 {
		for id, anger := range a.byUUID {
			if e, ok := tx.EntityByUUID(id); ok {
				a.suspects = append(a.suspects, &angerSuspect{e.H(), anger})
				delete(a.byUUID, id)
			}
		}
		a.conversionDelay = 2
	}
	for id, anger := range a.byUUID {
		if anger <= 1 {
			delete(a.byUUID, id)
		} else {
			a.byUUID[id] = anger - 1
		}
	}
	kept := a.suspects[:0]
	for _, s := range a.suspects {
		e, loaded := s.h.Entity(tx)
		if s.anger > 1 && loaded && valid(e) {
			s.anger--
			kept = append(kept, s)
			continue
		}
		if s.anger > 1 && !loaded && !s.h.Closed() {
			// The suspect left the world or was unloaded.
			a.byUUID[s.h.UUID()] = s.anger - 1
		}
	}
	clear(a.suspects[len(kept):])
	a.suspects = kept
	a.sort(tx)
}

// sort ports AngerManagement.sortAndUpdateHighestAnger.
func (a *angerManagement) sort(tx *world.Tx) {
	a.highestAnger = 0
	slices.SortStableFunc(a.suspects, func(x, y *angerSuspect) int {
		if ax, ay := wardenAngry(x.anger), wardenAngry(y.anger); ax != ay {
			if ax {
				return -1
			}
			return 1
		}
		if px, py := x.h.Type().EncodeEntity() == "minecraft:player", y.h.Type().EncodeEntity() == "minecraft:player"; px != py {
			if px {
				return -1
			}
			return 1
		}
		return y.anger - x.anger
	})
	for _, s := range a.suspects {
		a.highestAnger = max(a.highestAnger, s.anger)
	}
}

// increaseAnger ports AngerManagement.increaseAnger.
func (a *angerManagement) increaseAnger(e world.Entity, increment int) int {
	s := a.find(e)
	if s == nil {
		s = &angerSuspect{h: e.H(), anger: min(150, increment)}
		s.anger += a.byUUID[e.H().UUID()]
		delete(a.byUUID, e.H().UUID())
		a.suspects = append(a.suspects, s)
	} else {
		s.anger = min(150, s.anger+increment)
	}
	a.sort(nil)
	return s.anger
}

// clearAnger ports AngerManagement.clearAnger.
func (a *angerManagement) clearAnger(e world.Entity) {
	a.suspects = slices.DeleteFunc(a.suspects, func(s *angerSuspect) bool { return s.h == e.H() })
	a.sort(nil)
}

// activeAnger ports AngerManagement.getActiveAnger.
func (a *angerManagement) activeAnger(target world.Entity) int {
	if target == nil {
		return a.highestAnger
	}
	return a.angerOf(target)
}

// activeEntity ports AngerManagement.getActiveEntity: the top suspect the
// filter lets through.
func (a *angerManagement) activeEntity(tx *world.Tx, filter func(world.Entity) bool) world.Entity {
	for _, s := range a.suspects {
		if e, ok := s.h.Entity(tx); ok && filter(e) {
			return e
		}
	}
	return nil
}

var _ world.EntityVibrationListener = (*Warden)(nil)
