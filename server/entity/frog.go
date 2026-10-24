package entity

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// FrogType is the Java frog entity type, used as the real destination of tadpole metamorphosis.
var FrogType frogType

type frogType struct{}

func (frogType) EncodeEntity() string { return "minecraft:frog" }
func (frogType) Open(tx *world.Tx, h *world.EntityHandle, data *world.EntityData) world.Entity {
	if data.Data == nil {
		frogConfig{}.Apply(data)
	}
	return &Frog{Ent: &Ent{tx: tx, handle: h, data: data}}
}
func (frogType) BBox(world.Entity) cube.BBox { return cube.Box(-.25, 0, -.25, .25, .5, .25) }
func (frogType) DecodeNBT(nbt map[string]any, data *world.EntityData) {
	c := frogConfig{}
	if _, ok := nbt["Health"]; ok {
		c.health, c.healthSet = float64(nbtconv.Float32(nbt, "Health")), true
	}
	if _, ok := nbt["Variant"]; ok {
		c.variant = int(nbtconv.Int32(nbt, "Variant"))
	}
	c.age = int(nbtconv.Int32(nbt, "Age"))
	if brain, ok := nbt["Brain"].(map[string]any); ok {
		if memories, ok := brain["memories"].(map[string]any); ok {
			_, c.pregnant = memories["minecraft:is_pregnant"]
		}
	}
	c.Apply(data)
	data.Data.(*frogBehaviour).breeding.decodeNBT(nbt)
}
func (frogType) EncodeNBT(data *world.EntityData) map[string]any {
	b := data.Data.(*frogBehaviour)
	m := b.breeding.encodeNBT(map[string]any{"Health": float32(b.health.Health()), "Variant": int32(b.variant), "Age": int32(b.age)})
	if b.br.has(memIsPregnant) {
		m["Brain"] = map[string]any{"memories": map[string]any{"minecraft:is_pregnant": map[string]any{"value": map[string]any{}}}}
	}
	return m
}

type frogConfig struct {
	health              float64
	variant, age        int
	healthSet, pregnant bool
}

func (c frogConfig) Apply(data *world.EntityData) {
	h := c.health
	if !c.healthSet {
		h = 10
	}
	b := &frogBehaviour{health: NewHealthManager(h, 10), effects: NewEffectManager(), variant: c.variant, age: c.age, speed: 1}
	b.br = newFrogBrain(b)
	// FrogAi.initMemories.
	b.br.set(memLongJumpCooldownTicks, frogTimeBetweenLongJumps.min+rand.IntN(frogTimeBetweenLongJumps.max-frogTimeBetweenLongJumps.min+1))
	if c.pregnant {
		b.br.set(memIsPregnant, true)
	}
	data.Data = b
}

// frogPose is the pose of a frog that its animations follow.
type frogPose uint8

const (
	frogStanding frogPose = iota
	frogCroaking
	frogUsingTongue
	frogLongJumping
)

type frogBehaviour struct {
	health  *HealthManager
	effects *EffectManager
	ai      MobAI
	br      *Brain
	variant int
	speed   float64
	dead    bool
	// age is the breeding cooldown: frogs are never babies.
	age      int
	breeding breeding
	pose     frogPose
	// tongueTarget is Frog.DATA_TONGUE_TARGET_ID, what the frog reaches for
	// with its tongue.
	tongueTarget *world.EntityHandle
}

// Frog is a living amphibian runtime entity. Tadpoles convert into this runtime at Java's age threshold.
type Frog struct{ *Ent }

func (f *Frog) behaviour() *frogBehaviour { return f.Behaviour().(*frogBehaviour) }

// FinalizeSpawn ports Frog.finalizeSpawn: the biome selects the variant.
func (f *Frog) FinalizeSpawn(_ string, r *vanilla.Random) {
	if r == nil {
		return
	}
	b := f.behaviour()
	b.variant = frogVariantAt(f.tx, f.Position())
	b.br.set(memLongJumpCooldownTicks, frogTimeBetweenLongJumps.min+int(r.NextInt(int32(frogTimeBetweenLongJumps.max-frogTimeBetweenLongJumps.min+1))))
}

// Variant returns the Bedrock variant of the frog: 0 temperate, 1 cold, 2 warm.
func (f *Frog) Variant() int32 { return int32(f.behaviour().variant) }

// LootComponents exposes the frog variant (Bedrock 0 temperate, 1 cold, 2 warm)
// to loot predicates such as the magma cube froglight drop.
func (f *Frog) LootComponents() map[string]any {
	return map[string]any{"minecraft:frog/variant": "minecraft:" + []string{"temperate", "cold", "warm"}[max(0, min(f.behaviour().variant, 2))]}
}
func (f *Frog) Tick(tx *world.Tx, current int64) {
	f.behaviour().effects.Tick(f, tx)
	f.Ent.Tick(tx, current)
}

func (b *frogBehaviour) mobAI() *MobAI   { return &b.ai }
func (b *frogBehaviour) brain() *Brain   { return b.br }
func (b *frogBehaviour) panicking() bool { return b.br.has(memIsPanicking) }

// Tick runs a tick of the frog: its love and its MobAI with its brain.
func (b *frogBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	f := &Frog{Ent: e}
	// Frog.createAttributes and getHeadRotSpeed and getMaxHeadYRot.
	b.ai.MovementSpeed, b.ai.StepHeight = b.speed, 1
	b.ai.HeadRotSpeed, b.ai.MaxHeadYaw = 35, 5
	tickAgeing(f, tx)
	return b.ai.Tick(e, f, func(m *Mob) {
		m.AI.nav.SetFrog()
		m.AI.SetPathfindingMalus(PathWater, 4)
		m.AI.SetPathfindingMalus(PathTrapdoor, -1)
		m.AI.moveOverride = smoothSwimmingMoveControl{maxTurnX: 85, maxTurnY: 10, inWater: float64(float32(.02)), outsideWater: float64(float32(.1)), gravity: true}
	})
}

// customServerAiStep ports Frog.customServerAiStep and FrogAi.updateActivity.
func (b *frogBehaviour) customServerAiStep(m *Mob) {
	b.br.tick(m)
	b.br.setActiveToFirstValid(activityTongue, activityLaySpawn, activityLongJump, activitySwim, activityIdle)
	// FrogLookControl keeps the pitch of a frog reaching for its prey.
	m.AI.look.keepPitch = b.tongueTarget != nil
}

// travel ports Frog.travelInWater: a frog swims without sinking and keeps
// nine tenths of its speed.
func (b *frogBehaviour) travel(m *Mob, input mgl64.Vec3) bool {
	if !m.InWater() {
		return false
	}
	m.AI.moveRelative(m, m.AI.speed, input)
	m.AI.moveSelf(m, m.data.Vel)
	m.data.Vel = m.data.Vel.Mul(.9)
	return true
}

// doHurtTarget ports Mob.doHurtTarget for frogs, which bite for their attack
// damage of 10.
func (b *frogBehaviour) doHurtTarget(m *Mob, target world.Entity) bool {
	l, ok := target.(Living)
	if !ok {
		return false
	}
	_, hurt := l.Hurt(10, AttackDamageSource{Attacker: m.Self})
	return hurt
}

func (b *frogBehaviour) setPose(m *Mob, p frogPose) {
	if b.pose != p {
		b.pose = p
		m.updateState()
	}
}

func (b *frogBehaviour) setLongJumping(m *Mob, v bool) {
	if v {
		b.setPose(m, frogLongJumping)
	} else {
		b.setPose(m, frogStanding)
	}
}

func (b *frogBehaviour) setTongueTarget(m *Mob, e world.Entity) {
	var h *world.EntityHandle
	if e != nil {
		h = e.H()
	}
	if b.tongueTarget != h {
		b.tongueTarget = h
		m.updateState()
	}
}

// frogTimeBetweenLongJumps is FrogAi.TIME_BETWEEN_LONG_JUMPS.
var frogTimeBetweenLongJumps = uniformInt{100, 140}

// newFrogBrain ports FrogAi and the brain provider of Frog.
func newFrogBrain(f *frogBehaviour) *Brain {
	b := newBrain(nearestLivingEntitySensor{}, hurtBySensor{}, frogAttackablesSensor{},
		temptingSensor{items: func(_ *Mob, s item.Stack) bool { return inItemTag(s, "frog_food") }}, isInWaterSensor{})
	lookAtPlayers := func() behaviorControl { return newSetEntityLookTargetSometimes(isPlayerType, 6, uniformInt{30, 60}) }
	startAttacking := func() behaviorControl {
		return newStartAttacking(func(m *Mob) bool { return !m.Brain().has(memBreedTarget) }, func(m *Mob) world.Entity {
			return m.Brain().entity(m, memNearestAttackable)
		})
	}
	onGround := func() behaviorControl { return newOneShotIf(func(m *Mob) bool { return m.OnGround() }) }
	b.addActivity(activityCore, 0,
		newAnimalPanic(2, nil, nil, nil),
		newLookAtTargetSink(45, 90),
		newMoveToTargetSink(150, 250, nil),
		newCountDownCooldownTicks(memTemptationCooldownTicks),
		newCountDownCooldownTicks(memLongJumpCooldownTicks),
	)
	b.addActivityWith(activityIdle, []prioritizedBehavior{
		{0, lookAtPlayers()},
		{0, newAnimalMakeLove(FrogType, 1, 2)},
		{1, newFollowTemptation(constSpeed(1.25), nil, false)},
		{2, startAttacking()},
		{3, newTryFindLand(6, 1)},
		{4, newRunOne([]memoryCondition{{memWalkTarget, memAbsent}},
			weightedBehavior{newRandomStroll(1, true), 1},
			weightedBehavior{newSetWalkTargetFromLookTarget(nil, constSpeed(1), 3), 1},
			weightedBehavior{newFrogCroak(f), 3},
			weightedBehavior{onGround(), 2},
		)},
	}, []memoryCondition{{memLongJumpMidJump, memAbsent}, {memIsInWater, memAbsent}}, nil)
	b.addActivityWith(activitySwim, []prioritizedBehavior{
		{0, lookAtPlayers()},
		{1, newFollowTemptation(constSpeed(1.25), nil, false)},
		{2, startAttacking()},
		{3, newTryFindLand(8, 1.5)},
		{5, &gateBehavior{entry: []memoryCondition{{memWalkTarget, memAbsent}}, children: []weightedBehavior{
			{newRandomSwim(.75), 1},
			{newRandomStroll(1, true), 1},
			{newSetWalkTargetFromLookTarget(nil, constSpeed(1), 3), 1},
			{newOneShotIf(func(m *Mob) bool { return m.InWater() }), 5},
		}}},
	}, []memoryCondition{{memLongJumpMidJump, memAbsent}, {memIsInWater, memPresent}}, nil)
	b.addActivityWith(activityLaySpawn, []prioritizedBehavior{
		{0, lookAtPlayers()},
		{1, startAttacking()},
		{2, newTryFindLandNearLiquid(8, 1)},
		{3, newTryLaySpawnOnFluidNearLand(block.FrogSpawn{})},
		{4, newRunOne(nil,
			weightedBehavior{newRandomStroll(1, true), 2},
			weightedBehavior{newSetWalkTargetFromLookTarget(nil, constSpeed(1), 3), 1},
			weightedBehavior{newFrogCroak(f), 2},
			weightedBehavior{onGround(), 1},
		)},
	}, []memoryCondition{{memLongJumpMidJump, memAbsent}, {memIsPregnant, memPresent}}, nil)
	b.addActivityWith(activityTongue, []prioritizedBehavior{
		{0, newStopAttackingIfTargetInvalid(nil, nil, true)},
		{1, newFrogShootTongue(f)},
	}, []memoryCondition{{memAttackTarget, memPresent}}, []memoryType{memAttackTarget})
	b.addActivityWith(activityLongJump, []prioritizedBehavior{
		{0, newLongJumpMidJump(frogTimeBetweenLongJumps, "mob.frog.step")},
		{1, newLongJumpToPreferredBlock(frogTimeBetweenLongJumps, 2, 4, 3.5714288, func(*Mob) string { return "mob.frog.jump_to_block" },
			func(b world.Block) bool { return blockInTag(b, "frog_prefer_jump_to") }, .5, frogLandingSpot)},
	}, []memoryCondition{{memTemptingPlayer, memAbsent}, {memBreedTarget, memAbsent}, {memLongJumpCooldownTicks, memAbsent}, {memIsInWater, memAbsent}}, nil)
	return b
}

// frogLandingSpot ports FrogAi.isAcceptableLandingSpot: a frog lands out of
// water, gladly on lily pads and big dripleaves and beside trapdoors, and
// otherwise where any long jumper would.
func frogLandingSpot(m *Mob, pos cube.Pos) bool {
	below := pos.Side(cube.FaceDown)
	for _, p := range []cube.Pos{pos, below, pos.Side(cube.FaceUp)} {
		if _, liquid := m.tx.Liquid(p); liquid {
			return false
		}
	}
	b := m.tx.Block(pos)
	if blockInTag(b, "frog_prefer_jump_to") || blockInTag(m.tx.Block(below), "frog_prefer_jump_to") {
		return true
	}
	if pathTypeStatic(m.tx, pos) == PathTrapdoor || isAir(b) && pathTypeStatic(m.tx, below) == PathTrapdoor {
		return true
	}
	return defaultLongJumpLanding(m, pos)
}

// newFrogCroak ports Croak: a frog standing out of liquid croaks for three
// seconds.
func newFrogCroak(f *frogBehaviour) *behavior {
	counter := 0
	return &behavior{
		entry:       []memoryCondition{{memWalkTarget, memAbsent}},
		minDuration: 100, maxDuration: 100,
		checkExtra:  func(*Mob) bool { return f.pose == frogStanding },
		canStillUse: func(*Mob, int64) bool { return counter < 60 },
		start: func(m *Mob, _ int64) {
			if !m.InLiquid() {
				f.setPose(m, frogCroaking)
				counter = 0
			}
		},
		stop: func(m *Mob, _ int64) { f.setPose(m, frogStanding) },
		tick: func(*Mob, int64) { counter++ },
	}
}

// frogTongueState is ShootTongue.State.
type frogTongueState uint8

const (
	frogTongueMove frogTongueState = iota
	frogTongueCatch
	frogTongueEat
	frogTongueDone
)

// newFrogShootTongue ports ShootTongue: a frog hops up to the prey it
// targets, pulls it in with its tongue from within 1.75 blocks and eats it.
// Prey it finds no path to is left alone for five seconds.
func newFrogShootTongue(f *frogBehaviour) *behavior {
	var (
		state       = frogTongueDone
		timer       int
		pathCounter int
	)
	walkTo := func(m *Mob, target world.Entity) {
		m.Brain().set(memWalkTarget, walkTarget{target: newBlockPosTracker(cube.PosFromVec3(target.Position())), speed: 2})
	}
	return &behavior{
		entry: []memoryCondition{{memWalkTarget, memAbsent}, {memLookTarget, memRegistered}, {memAttackTarget, memPresent},
			{memIsPanicking, memAbsent}},
		minDuration: 100, maxDuration: 100,
		checkExtra: func(m *Mob) bool {
			b := m.Brain()
			target := b.entity(m, memAttackTarget)
			if target == nil {
				return false
			}
			path := m.AI.nav.CreatePathToEntity(m, target, 0)
			if path == nil || path.distToTarget >= 1.75 {
				b.erase(memAttackTarget)
				frogForgetTongueTarget(m, target)
				return false
			}
			return f.pose != frogCroaking && frogCanEat(target)
		},
		canStillUse: func(m *Mob, _ int64) bool {
			b := m.Brain()
			return b.has(memAttackTarget) && state != frogTongueDone && !b.has(memIsPanicking)
		},
		start: func(m *Mob, _ int64) {
			target := m.Brain().entity(m, memAttackTarget)
			m.Brain().set(memLookTarget, newEntityTracker(target, true))
			f.setTongueTarget(m, target)
			walkTo(m, target)
			pathCounter, state = 10, frogTongueMove
		},
		stop: func(m *Mob, _ int64) {
			m.Brain().erase(memAttackTarget)
			f.setTongueTarget(m, nil)
			f.setPose(m, frogStanding)
		},
		tick: func(m *Mob, _ int64) {
			target := m.Brain().entity(m, memAttackTarget)
			if target == nil {
				return
			}
			f.setTongueTarget(m, target)
			switch state {
			case frogTongueMove:
				switch {
				case target.Position().Sub(m.data.Pos).Len() < 1.75:
					m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "mob.frog.tongue", Volume: 2, Pitch: 1})
					f.setPose(m, frogUsingTongue)
					if v, ok := target.(interface{ SetVelocity(mgl64.Vec3) }); ok {
						if pull := m.data.Pos.Sub(target.Position()); pull.LenSqr() > 0 {
							v.SetVelocity(pull.Normalize().Mul(.75))
						}
					}
					timer, state = 0, frogTongueCatch
				case pathCounter <= 0:
					walkTo(m, target)
					pathCounter = 10
				default:
					pathCounter--
				}
			case frogTongueCatch:
				timer++
				if timer-1 >= 6 {
					state = frogTongueEat
					frogEat(m, f)
				}
			case frogTongueEat:
				if timer >= 10 {
					state = frogTongueDone
				} else {
					timer++
				}
			}
		},
	}
}

// frogEat ports ShootTongue.eatEntity: the frog bites what its tongue holds,
// and prey it kills disappears at once.
func frogEat(m *Mob, f *frogBehaviour) {
	m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "mob.frog.eat", Volume: 2, Pitch: 1})
	if f.tongueTarget == nil {
		return
	}
	target, ok := f.tongueTarget.Entity(m.tx)
	if !ok || !entityAlive(target) {
		return
	}
	m.doHurtTarget(target)
	if _, ok := target.H().Entity(m.tx); ok && !entityAlive(target) {
		_ = target.(interface{ CloseIn(*world.Tx) error }).CloseIn(m.tx)
	}
}

// frogForgetTongueTarget ports ShootTongue.addUnreachableTargetToMemory: a
// frog remembers up to five prey it found no path to for five seconds.
func frogForgetTongueTarget(m *Mob, target world.Entity) {
	b := m.Brain()
	var list []*world.EntityHandle
	if v, ok := b.get(memUnreachableTongueTargets); ok {
		list = v.([]*world.EntityHandle)
	}
	known := false
	for _, h := range list {
		known = known || h == target.H()
	}
	if !known {
		if len(list) == 5 {
			list = list[1:]
		}
		list = append(list, target.H())
	}
	b.setWithExpiry(memUnreachableTongueTargets, list, 100)
}

// frogCanEat ports Frog.canEat: a slime or magma cube of size 1.
func frogCanEat(e world.Entity) bool {
	if c, ok := e.(interface{ CubeSize() int }); ok && c.CubeSize() != 1 {
		return false
	}
	return entityTypeInTag(e, "frog_food")
}

// frogAttackablesSensor ports FrogAttackablesSensor: the nearest prey within
// 10 blocks a frog sees, may attack and has not found unreachable.
type frogAttackablesSensor struct{}

func (frogAttackablesSensor) scanRate() int { return 0 }
func (frogAttackablesSensor) requires() []memoryType {
	return []memoryType{memNearestAttackable, memNearestVisibleLivingEntities, memUnreachableTongueTargets}
}

func (frogAttackablesSensor) doTick(m *Mob, b *Brain) {
	var unreachable []*world.EntityHandle
	if v, ok := b.get(memUnreachableTongueTargets); ok {
		unreachable = v.([]*world.EntityHandle)
	}
	var nearest world.Entity
	if v := visibleLiving(m); v != nil {
		nearest = v.findClosest(m, func(e world.Entity) bool {
			for _, h := range unreachable {
				if h == e.H() {
					return false
				}
			}
			return sensorAttackable(m, e) && frogCanEat(e) && m.DistanceToSqr(e) < 100
		})
	}
	b.setEntity(memNearestAttackable, nearest)
}

// newTryLaySpawnOnFluidNearLand ports TryLaySpawnOnFluidNearLand: a pregnant
// mob walking on land lays its spawn on water beside the block it stands on.
func newTryLaySpawnOnFluidNearLand(spawn world.Block) *oneShot {
	return newOneShot([]memoryCondition{{memAttackTarget, memAbsent}, {memWalkTarget, memPresent}, {memIsPregnant, memPresent}}, func(m *Mob, b *Brain, _ int64) bool {
		if m.InWater() || !m.OnGround() {
			return false
		}
		below := m.BlockPos().Side(cube.FaceDown)
		for _, d := range horizontalDirections {
			rel := below.Add(d)
			if !faceUpEmpty(m.tx, rel) || !pathWater(m.tx, rel) {
				continue
			}
			site := rel.Side(cube.FaceUp)
			if !isAir(m.tx.Block(site)) {
				continue
			}
			m.tx.SetBlock(site, spawn, nil)
			m.tx.EmitGameEvent(world.GameEventBlockPlace, site.Vec3Centre(), world.GameEventContext{Source: m.Self, AffectedState: spawn})
			m.tx.PlaySound(m.data.Pos, sound.NativeEvent{SoundType: "lay_spawn", EntityType: "minecraft:frog", ExtraData: -1})
			b.erase(memIsPregnant)
			return true
		}
		return true
	})
}

// faceUpEmpty reports if the collision shape of the block at pos leaves its
// top face open, as VoxelShape.getFaceShape(UP).isEmpty.
func faceUpEmpty(tx *world.Tx, pos cube.Pos) bool {
	for _, bb := range tx.Block(pos).Model().BBox(pos, tx) {
		if bb.Max()[1] >= 1-1e-7 {
			return false
		}
	}
	return true
}

func (f *Frog) Health() float64        { return f.behaviour().health.Health() }
func (f *Frog) MaxHealth() float64     { return f.behaviour().health.MaxHealth() }
func (f *Frog) SetMaxHealth(v float64) { f.behaviour().health.SetMaxHealth(v) }
func (f *Frog) Dead() bool             { return f.behaviour().dead || f.Health() <= 0 }

// Panicking reports whether the frog runs from danger.
func (f *Frog) Panicking() bool { return f.behaviour().panicking() }
func (f *Frog) Hurt(d float64, src world.DamageSource) (float64, bool) {
	if f.Dead() || d <= 0 {
		return 0, false
	}
	b := f.behaviour()
	d, ok := b.ai.DamageCooldown(f.tx, d)
	if !ok {
		return 0, false
	}
	if d, ok = mobEffectDamage(b.effects, d, src); !ok {
		return 0, false
	}
	before := b.health.Health()
	b.health.AddHealth(-d)
	b.ai.Hurt(src)
	resetLove(f)
	dealt := before - b.health.Health()
	f.viewAction(HurtAction{})
	onMobHurt(f.tx, f)
	if f.Dead() {
		b.dead = true
		f.viewAction(DeathAction{})
		dropAllDeathLoot(f.tx, f, src)
		_ = f.CloseIn(f.tx)
	}
	return dealt, true
}
func (f *Frog) Heal(h float64, _ world.HealingSource) float64 {
	if f.Dead() || h <= 0 {
		return 0
	}
	before := f.Health()
	f.behaviour().health.AddHealth(h)
	return f.Health() - before
}
func (f *Frog) KnockBack(src mgl64.Vec3, force, height float64) {
	d := f.Position().Sub(src)
	d[1] = 0
	if d.Len() != 0 {
		d = d.Normalize().Mul(force)
	}
	d[1] = height
	f.SetVelocity(d)
}
func (f *Frog) AddEffect(e effect.Effect)  { f.behaviour().effects.Add(e, f) }
func (f *Frog) RemoveEffect(t effect.Type) { f.behaviour().effects.Remove(t, f) }
func (f *Frog) Effects() []effect.Effect   { return f.behaviour().effects.Effects() }
func (f *Frog) Speed() float64             { return f.behaviour().speed }
func (f *Frog) SetSpeed(v float64)         { f.behaviour().speed = v }
func (f *Frog) viewAction(action world.EntityAction) {
	viewers := f.tx.Viewers(f.Position())
	for _, v := range viewers {
		v.ViewEntityAction(f, action)
	}
	f.tx.ReleaseViewers(viewers)
}

// Croaking reports if the frog croaks, as Bedrock shows.
func (f *Frog) Croaking() bool { return f.behaviour().pose == frogCroaking }

// EatingMob reports if the frog pulls in prey with its tongue, as Bedrock
// shows.
func (f *Frog) EatingMob() bool { return f.behaviour().pose == frogUsingTongue }

// JumpGoalJumping reports if the frog is in a long jump, as Bedrock shows.
func (f *Frog) JumpGoalJumping() bool { return f.behaviour().pose == frogLongJumping }

// TongueTargetHandle returns what the frog reaches for with its tongue, or
// nil.
func (f *Frog) TongueTargetHandle() *world.EntityHandle { return f.behaviour().tongueTarget }

func NewFrog(opts world.EntitySpawnOpts) *world.EntityHandle { return opts.New(FrogType, frogConfig{}) }

var _ world.EntityType = frogType{}
var _ Living = (*Frog)(nil)

// Interact feeds the frog slime balls to breed it.
func (f *Frog) Interact(_ *world.Tx, u item.User, ctx *item.UseContext) bool {
	if !feedBreeder(f, u, ctx) {
		return false
	}
	// Frog.playEatingSound.
	f.tx.PlaySound(f.Position(), sound.Custom{Name: "mob.frog.eat", Volume: 2, Pitch: 1})
	return true
}

// InLove reports whether the frog is in love.
func (f *Frog) InLove() bool { return f.behaviour().breeding.InLove() }

// Pregnant reports whether the frog bred and is yet to lay frogspawn.
func (f *Frog) Pregnant() bool { return f.behaviour().br.has(memIsPregnant) }

func (f *Frog) breedingState() *breeding { return &f.behaviour().breeding }
func (f *Frog) ageTicks() *int           { return &f.behaviour().age }

func (f *Frog) breedingFood(stack item.Stack) bool { return inItemTag(stack, "frog_food") }

func (f *Frog) breedOffspring(*world.Tx, breeder) *world.EntityHandle { return nil }

// spawnChildFromBreeding ports Frog.spawnChildFromBreeding: instead of a
// baby, the frog becomes pregnant and lays frogspawn later.
func (f *Frog) spawnChildFromBreeding(tx *world.Tx, partner breeder) {
	finishBreeding(f, partner, tx)
	f.behaviour().br.set(memIsPregnant, true)
}

// walkTargetValue ports Animal.getWalkTargetValue.
func (b *frogBehaviour) walkTargetValue(m *Mob, pos cube.Pos) float64 {
	return animalWalkTargetValue(m, pos)
}
