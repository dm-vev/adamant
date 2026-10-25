package entity

import (
	"math"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// NewGoat creates a normal adult goat.
func NewGoat(o world.EntitySpawnOpts) *world.EntityHandle { return o.New(GoatType, GoatConfig{}) }

// GoatType is the registered Java goat type.
var GoatType goatType

type goatType struct{}

func (goatType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	if d.Data == nil {
		GoatConfig{}.Apply(d)
	}
	return &Goat{Ent: &Ent{tx: tx, handle: h, data: d}}
}
func (goatType) EncodeEntity() string { return "minecraft:goat" }
func (goatType) BBox(e world.Entity) cube.BBox {
	w, h := .45, 1.3
	if g, ok := e.(*Goat); ok {
		b := g.behaviour()
		if b.age < 0 {
			w, h = .225, .65
		}
		if b.longJumping {
			// Goat.LONG_JUMPING_DIMENSION_SCALE_FACTOR.
			w, h = w*.7, h*.7
		}
	}
	return cube.Box(-w, 0, -w, w, h, w)
}
func (goatType) DecodeNBT(m map[string]any, d *world.EntityData) {
	s := ReadNativeMobJavaState(m)
	if s == nil {
		s = m
	}
	c := GoatConfig{Age: int(nbtconv.Int32(s, "Age")), Screaming: nbtconv.Bool(s, "IsScreamingGoat"), LeftHorn: true, RightHorn: true}
	if _, ok := s["HasLeftHorn"]; ok {
		c.LeftHorn = nbtconv.Bool(s, "HasLeftHorn")
	}
	if _, ok := s["HasRightHorn"]; ok {
		c.RightHorn = nbtconv.Bool(s, "HasRightHorn")
	}
	if a, ok := nativeMobAttribute(m, "minecraft:health"); ok {
		c.Health, c.MaxHealth = a.Current, a.Base
	}
	c.hornsSet = true
	d.Data = c.New()
	d.Data.(*GoatBehaviour).breeding.decodeNBT(s)
}
func (goatType) EncodeNBT(d *world.EntityData) map[string]any {
	b := d.Data.(*GoatBehaviour)
	a := EncodeNativeMobAttributes(ReadNativeMobAttributes(d.NativeNBT), []NativeMobAttribute{{Name: "minecraft:health", Base: b.maxHealth, Current: b.health, DefaultMin: 0, DefaultMax: b.maxHealth, Min: 0, Max: b.maxHealth}})
	return MergeNativeMobJavaState(map[string]any{"Attributes": a}, MergeNativeMobNBT(ReadNativeMobJavaState(d.NativeNBT), b.breeding.encodeNBT(map[string]any{"Age": int32(b.age), "IsScreamingGoat": byte(boolByte(b.screaming)), "HasLeftHorn": byte(boolByte(b.leftHorn)), "HasRightHorn": byte(boolByte(b.rightHorn))})))
}

// GoatConfig configures source goat state.
type GoatConfig struct {
	Health, MaxHealth              float64
	Age                            int
	Screaming, LeftHorn, RightHorn bool
	hornsSet                       bool
}

func (c GoatConfig) Apply(d *world.EntityData) { d.Data = c.New() }
func (c GoatConfig) New() *GoatBehaviour {
	if c.MaxHealth <= 0 {
		c.MaxHealth = 10
	}
	if c.Health <= 0 {
		c.Health = c.MaxHealth
	}
	if !c.hornsSet && !c.LeftHorn && !c.RightHorn {
		c.LeftHorn, c.RightHorn = true, true
	}
	b := &GoatBehaviour{health: c.Health, maxHealth: c.MaxHealth, age: c.Age, screaming: c.Screaming, leftHorn: c.LeftHorn, rightHorn: c.RightHorn, speed: .2, effects: NewEffectManager()}
	b.br = newGoatBrain(b)
	// GoatAi.initMemories.
	b.br.set(memLongJumpCooldownTicks, 600+rand.IntN(601))
	b.br.set(memRamCooldownTicks, 600+rand.IntN(5401))
	return b
}

// Goat is a mountain animal that leaps between ledges and rams what stands
// in its way.
type Goat struct{ *Ent }
type GoatBehaviour struct {
	effects                              *EffectManager
	ai                                   MobAI
	br                                   *Brain
	health, maxHealth                    float64
	age                                  int
	screaming, leftHorn, rightHorn, dead bool
	// loweringHead and longJumping are shown to viewers as the goat lowers
	// its head to ram and leaps.
	loweringHead, longJumping bool
	speed                     float64
	breeding                  breeding
}

func (g *Goat) behaviour() *GoatBehaviour { return g.Behaviour().(*GoatBehaviour) }
func (g *Goat) Tick(tx *world.Tx, t int64) {
	g.behaviour().effects.Tick(g, tx)
	g.Ent.Tick(tx, t)
}

func (b *GoatBehaviour) mobAI() *MobAI   { return &b.ai }
func (b *GoatBehaviour) brain() *Brain   { return b.br }
func (b *GoatBehaviour) panicking() bool { return b.br.has(memIsPanicking) }
func (b *GoatBehaviour) attackDamage() float64 {
	// Goat.ageBoundaryReached: kids butt for a point, adults for two.
	if b.age < 0 {
		return 1
	}
	return 2
}

// Tick runs a tick of the goat: its ageing and love, and its MobAI with its
// brain.
func (b *GoatBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	g := &Goat{Ent: e}
	b.ai.MovementSpeed, b.ai.EyeHeight = b.speed, 1.105
	if b.age < 0 {
		b.ai.EyeHeight = .59375
	}
	if b.ai.started {
		// Goat.getMaxHeadYRot.
		b.ai.MaxHeadYaw = 15
	}
	tickAgeing(g, tx)
	return b.ai.Tick(e, g, func(m *Mob) {
		m.AI.nav.SetCanFloat(true)
		m.AI.SetPathfindingMalus(PathPowderSnow, -1)
		m.AI.SetPathfindingMalus(PathOnTopOfPowderSnow, -1)
	})
}

// customServerAiStep ports Goat.customServerAiStep.
func (b *GoatBehaviour) customServerAiStep(m *Mob) {
	b.br.tick(m)
	b.br.setActiveToFirstValid(activityRam, activityLongJump, activityIdle)
}

func (b *GoatBehaviour) setLongJumping(m *Mob, v bool) {
	if b.longJumping != v {
		b.longJumping = v
		m.updateState()
	}
}

// setLoweringHead shows viewers the goat lowering its head to ram, as
// entity events 58 and 59.
func (b *GoatBehaviour) setLoweringHead(m *Mob, v bool) {
	if b.loweringHead != v {
		b.loweringHead = v
		m.updateState()
	}
}

func (b *GoatBehaviour) sound(normal, screamer string) string {
	if b.screaming {
		return screamer
	}
	return normal
}

// goatRamTargetable ports GoatAi.RAM_TARGET_CONDITIONS: a goat rams anything
// it may attack but other goats, and armour stands only with mob griefing.
func goatRamTargetable(m *Mob, e world.Entity) bool {
	if e.H().Type() == GoatType {
		return false
	}
	if e.H().Type().EncodeEntity() == "minecraft:armor_stand" && mobGriefingDisabled(m.tx) {
		return false
	}
	c := forCombat()
	return c.test(m, e)
}

// newGoatBrain ports GoatAi and the brain provider of Goat.
func newGoatBrain(g *GoatBehaviour) *Brain {
	b := newBrain(nearestLivingEntitySensor{}, playerSensor{}, nearestItemSensor{wants: func(*Mob, item.Stack) bool { return false }},
		adultSensor{}, hurtBySensor{}, temptingSensor{items: animalTemptation})
	jumps, rams, screamerRams := uniformInt{600, 1200}, uniformInt{600, 6000}, uniformInt{100, 300}
	ramsOf := func() uniformInt {
		if g.screaming {
			return screamerRams
		}
		return rams
	}
	b.addActivity(activityCore, 0,
		newSwim(.8),
		newAnimalPanic(2, nil, nil, nil),
		newLookAtTargetSink(45, 90),
		newMoveToTargetSink(150, 250, nil),
		newCountDownCooldownTicks(memTemptationCooldownTicks),
		newCountDownCooldownTicks(memLongJumpCooldownTicks),
		newCountDownCooldownTicks(memRamCooldownTicks),
	)
	b.addActivityWith(activityIdle, []prioritizedBehavior{
		{0, newSetEntityLookTargetSometimes(isPlayerType, 6, uniformInt{30, 60})},
		{0, newAnimalMakeLove(GoatType, 1, 2)},
		{1, newFollowTemptation(constSpeed(1.25), nil, false)},
		{2, newBabyFollowAdult(uniformInt{5, 16}, constSpeed(1.25), memNearestVisibleAdult, false)},
		{3, newRunOne(nil,
			weightedBehavior{newRandomStroll(1, true), 2},
			weightedBehavior{newSetWalkTargetFromLookTarget(nil, constSpeed(1), 3), 2},
			weightedBehavior{newDoNothing(30, 60), 1},
		)},
	}, []memoryCondition{{memRamCooldownTicks, memPresent}, {memLongJumpCooldownTicks, memPresent}}, nil)
	b.addActivityWith(activityLongJump, []prioritizedBehavior{
		{0, newLongJumpMidJump(jumps, "mob.goat.step")},
		{1, newLongJumpToRandomPos(jumps, 5, 5, 3.5714288, func(*Mob) string { return "" }, nil)},
	}, []memoryCondition{{memTemptingPlayer, memAbsent}, {memBreedTarget, memAbsent}, {memWalkTarget, memAbsent}, {memLongJumpCooldownTicks, memAbsent}}, nil)
	b.addActivityWith(activityRam, []prioritizedBehavior{
		{0, newGoatRamTarget(g, ramsOf)},
		{1, newPrepareRamNearestTarget(g, func() int { return ramsOf().min }, 4, 7, 1.25, 20)},
	}, []memoryCondition{{memTemptingPlayer, memAbsent}, {memBreedTarget, memAbsent}, {memRamCooldownTicks, memAbsent}}, nil)
	return b
}

// ramCandidate ports PrepareRamNearestTarget.RamCandidate.
type ramCandidate struct {
	start, targetPos cube.Pos
	target           *world.EntityHandle
}

// newPrepareRamNearestTarget ports PrepareRamNearestTarget: a goat picks the
// nearest thing it may ram, walks to a spot four to seven blocks from it in
// a straight line, lowers its head there for a second and then charges at
// it. If it finds no such spot, it waits a while before it tries again.
func newPrepareRamNearestTarget(g *GoatBehaviour, cooldownOnFail func() int, minDist, maxDist int, walkSpeed float64, prepareTime int64) *behavior {
	var (
		reachedAt *int64
		candidate *ramCandidate
	)
	walkable := func(m *Mob, pos cube.Pos) bool {
		return m.AI.nav.IsStableDestination(m.tx, pos) && m.PathfindingMalus(pathTypeStatic(m.tx, pos)) == 0
	}
	startPos := func(m *Mob, target world.Entity) (cube.Pos, bool) {
		tp := cube.PosFromVec3(target.Position())
		if !walkable(m, tp) {
			return cube.Pos{}, false
		}
		var positions []cube.Pos
		for _, f := range []cube.Face{cube.FaceNorth, cube.FaceEast, cube.FaceSouth, cube.FaceWest} {
			p := tp
			for range maxDist {
				if next := p.Side(f); walkable(m, next) {
					p = next
				} else {
					break
				}
			}
			if manhattan(p, tp) >= minDist {
				positions = append(positions, p)
			}
		}
		at := m.BlockPos()
		for len(positions) > 0 {
			best := 0
			for i, p := range positions {
				if distSqrPos(at, p) < distSqrPos(at, positions[best]) {
					best = i
				}
			}
			p := positions[best]
			positions = append(positions[:best], positions[best+1:]...)
			if path := m.AI.nav.CreatePath(m, p, 0); path != nil && path.CanReach() {
				return p, true
			}
		}
		return cube.Pos{}, false
	}
	choose := func(m *Mob, target world.Entity) {
		reachedAt = nil
		candidate = nil
		if p, ok := startPos(m, target); ok {
			candidate = &ramCandidate{start: p, targetPos: cube.PosFromVec3(target.Position()), target: target.H()}
		}
	}
	targetOf := func(m *Mob) world.Entity {
		if candidate == nil {
			return nil
		}
		e, ok := candidate.target.Entity(m.tx)
		if !ok || !entityAlive(e) {
			return nil
		}
		return e
	}
	return &behavior{
		entry: []memoryCondition{{memLookTarget, memRegistered}, {memRamCooldownTicks, memAbsent}, {memNearestVisibleLivingEntities, memPresent},
			{memRamTarget, memAbsent}},
		minDuration: 160, maxDuration: 160,
		start: func(m *Mob, _ int64) {
			candidate = nil
			if t := visibleLiving(m).findClosest(m, func(e world.Entity) bool { return goatRamTargetable(m, e) }); t != nil {
				choose(m, t)
			}
		},
		stop: func(m *Mob, _ int64) {
			if !m.Brain().has(memRamTarget) {
				g.setLoweringHead(m, false)
				m.Brain().set(memRamCooldownTicks, cooldownOnFail())
			}
		},
		canStillUse: func(m *Mob, _ int64) bool { return targetOf(m) != nil },
		tick: func(m *Mob, time int64) {
			target := targetOf(m)
			if target == nil {
				return
			}
			b := m.Brain()
			b.set(memWalkTarget, walkTarget{target: newBlockPosTracker(candidate.start), speed: walkSpeed})
			b.set(memLookTarget, newEntityTracker(target, true))
			if cube.PosFromVec3(target.Position()) != candidate.targetPos {
				g.setLoweringHead(m, false)
				m.AI.nav.Stop()
				choose(m, target)
				return
			}
			at := m.BlockPos()
			if at != candidate.start {
				return
			}
			g.setLoweringHead(m, true)
			if reachedAt == nil {
				reachedAt = &time
			}
			if time-*reachedAt >= prepareTime {
				tp := candidate.targetPos
				edge := tp.Vec3Middle().Add(mgl64.Vec3{.5 * signum(float64(tp[0]-at[0])), 0, .5 * signum(float64(tp[2]-at[2]))})
				b.set(memRamTarget, edge)
				r := m.Random()
				m.tx.PlaySound(m.data.Pos, sound.Custom{Name: g.sound("mob.goat.prepare_ram", "mob.goat.prepare_ram.screamer"), Volume: 1, Pitch: float64((r.NextFloat()-r.NextFloat())*.2 + 1)})
				candidate = nil
			}
		},
	}
}

// newGoatRamTarget ports RamTarget: a goat charges at three times its speed
// to where it aimed, butting the first thing it runs into far away, or
// snapping off a horn on stone, ores and the like.
func newGoatRamTarget(g *GoatBehaviour, between func() uniformInt) *behavior {
	var dir mgl64.Vec3
	finish := func(m *Mob) {
		g.setLoweringHead(m, false)
		m.Brain().set(memRamCooldownTicks, between().sample(m))
		m.Brain().erase(memRamTarget)
	}
	return &behavior{
		entry:       []memoryCondition{{memRamCooldownTicks, memAbsent}, {memRamTarget, memPresent}},
		minDuration: 200, maxDuration: 200,
		checkExtra:  func(m *Mob) bool { return m.Brain().has(memRamTarget) },
		canStillUse: func(m *Mob, _ int64) bool { return m.Brain().has(memRamTarget) },
		start: func(m *Mob, _ int64) {
			at := m.BlockPos()
			v, _ := m.Brain().get(memRamTarget)
			target := v.(mgl64.Vec3)
			dir = mgl64.Vec3{float64(at[0]) - target[0], 0, float64(at[2]) - target[2]}
			if dir.LenSqr() > 0 {
				dir = dir.Normalize()
			}
			// WalkTarget(Vec3) walks to the centre of the block of the spot.
			m.Brain().set(memWalkTarget, walkTarget{target: newBlockPosTracker(cube.PosFromVec3(target)), speed: 3})
		},
		tick: func(m *Mob, _ int64) {
			var hit world.Entity
			box := m.BBox()
			for e := range m.tx.EntitiesWithin(box.Grow(2)) {
				if e.H() != m.H() && goatRamTargetable(m, e) && e.H().Type().BBox(e).Translate(e.Position()).IntersectsWith(box) {
					hit = e
					break
				}
			}
			impact := g.sound("mob.goat.ram_impact", "mob.goat.ram_impact.screamer")
			switch {
			case hit != nil:
				damage := g.attackDamage()
				l := hit.(Living)
				_, hurt := l.Hurt(damage, AttackDamageSource{Attacker: m.Self})
				speedBoost := 0.0
				if e, ok := mobEffect(m, effect.Speed); ok {
					speedBoost += float64(e.Level())
				}
				if e, ok := mobEffect(m, effect.Slowness); ok {
					speedBoost -= float64(e.Level())
				}
				factor := math.Max(.2, math.Min(3, m.AI.speed*1.65)) + .25*speedBoost
				blocking := 1.0
				if b, ok := hit.(interface{ Blocking() bool }); ok && b.Blocking() && !hurt {
					blocking = .5
				}
				force := 2.5
				if g.age < 0 {
					force = 1
				}
				knockBackFrom(hit, dir, blocking*factor*force)
				finish(m)
				m.tx.PlaySound(m.data.Pos, sound.Custom{Name: impact, Volume: 1, Pitch: 1})
			case goatRammedHornBreaker(m):
				m.tx.PlaySound(m.data.Pos, sound.Custom{Name: impact, Volume: 1, Pitch: 1})
				if (&Goat{Ent: m.Ent}).dropHorn() {
					m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "mob.goat.horn_break", Volume: 1, Pitch: 1})
				}
				finish(m)
			default:
				b := m.Brain()
				wv, wok := b.get(memWalkTarget)
				rv, rok := b.get(memRamTarget)
				if !wok || !rok || wv.(walkTarget).target.currentPosition(m.tx).Sub(rv.(mgl64.Vec3)).LenSqr() < .25*.25 {
					finish(m)
				}
			}
		},
	}
}

// knockBackFrom ports LivingEntity.knockback with a direction: the entity is
// pushed away along the opposite of dir, xd and zd of Java, by power.
func knockBackFrom(e world.Entity, dir mgl64.Vec3, power float64) {
	if k, ok := e.(interface {
		KnockBack(mgl64.Vec3, float64, float64)
	}); ok {
		// KnockBack pushes away from a source, here the side dir points to,
		// and up by the power, at most 0.4.
		k.KnockBack(e.Position().Add(dir), power, math.Min(.4, power))
	}
}

// goatRammedHornBreaker ports RamTarget.hasRammedHornBreakingBlock: the
// block the goat runs into, or the one above it, is in #snaps_goat_horn.
func goatRammedHornBreaker(m *Mob) bool {
	v := mgl64.Vec3{m.data.Vel[0], 0, m.data.Vel[2]}
	if v.LenSqr() > 0 {
		v = v.Normalize()
	}
	facing := cube.PosFromVec3(m.data.Pos.Add(v))
	return blockInTag(m.tx.Block(facing), "snaps_goat_horn") || blockInTag(m.tx.Block(facing.Side(cube.FaceUp)), "snaps_goat_horn")
}

// dropHorn ports Goat.dropHorn: an adult goat loses a horn, at random if it
// has both, which drops as a goat horn.
func (g *Goat) dropHorn() bool {
	b := g.behaviour()
	if b.age < 0 || !b.leftHorn && !b.rightHorn {
		return false
	}
	m := b.ai.Mob(g.Ent, g)
	r := m.Random()
	left := !b.rightHorn || b.leftHorn && r.NextInt(2) == 0
	if left {
		b.leftHorn = false
	} else {
		b.rightHorn = false
	}
	g.updateState()
	vel := mgl64.Vec3{float64(r.NextFloat())*.4 - .2, float64(r.NextFloat())*.4 + .3, float64(r.NextFloat())*.4 - .2}
	g.tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: g.Position(), Velocity: vel}, item.NewStack(item.GoatHorn{Type: g.hornType()}, 1)))
	return true
}

// hornType ports Goat.createHorn: the instrument a goat's horn plays, picked
// by its UUID among the regular or the screaming goat horns.
func (g *Goat) hornType() sound.Horn {
	horns := sound.GoatHorns()
	id := g.H().UUID()
	r := vanilla.NewLegacyRandom(int64(int32(uuidHashCode(id))))
	if g.behaviour().screaming {
		return horns[4+int(r.NextInt(4))]
	}
	return horns[int(r.NextInt(4))]
}

func (g *Goat) Health() float64    { return g.behaviour().health }
func (g *Goat) MaxHealth() float64 { return g.behaviour().maxHealth }
func (g *Goat) SetMaxHealth(v float64) {
	b := g.behaviour()
	b.maxHealth = v
	b.health = math.Min(v, b.health)
}
func (g *Goat) Dead() bool { return g.behaviour().dead || g.Health() <= 0 }

// Panicking reports whether the goat runs from danger.
func (g *Goat) Panicking() bool { return g.behaviour().panicking() }
func (g *Goat) Hurt(v float64, src world.DamageSource) (float64, bool) {
	if g.Dead() || v <= 0 {
		return 0, false
	}
	b := g.behaviour()
	v, ok := b.ai.DamageCooldown(g.tx, v)
	if !ok {
		return 0, false
	}
	if v, ok = mobEffectDamage(b.effects, v, src); !ok {
		return 0, false
	}
	b.health -= v
	b.ai.Hurt(src)
	resetLove(g)
	g.viewAction(HurtAction{})
	onMobHurt(g.tx, g)
	if g.Dead() {
		b.dead = true
		g.viewAction(DeathAction{})
		dropAllDeathLoot(g.tx, g, src)
		_ = g.CloseIn(g.tx)
	}
	return v, true
}
func (g *Goat) Heal(v float64, _ world.HealingSource) float64 {
	if g.Dead() || v <= 0 {
		return 0
	}
	b := g.behaviour()
	old := b.health
	b.health = math.Min(b.maxHealth, b.health+v)
	return b.health - old
}
func (g *Goat) KnockBack(src mgl64.Vec3, f, h float64) {
	knockBack(g, src, f, h)
}
func (g *Goat) viewAction(a world.EntityAction) {
	vs := g.tx.Viewers(g.Position())
	for _, v := range vs {
		v.ViewEntityAction(g, a)
	}
	g.tx.ReleaseViewers(vs)
}
func (g *Goat) Screaming() bool     { return g.behaviour().screaming }
func (g *Goat) Horns() (bool, bool) { b := g.behaviour(); return b.leftHorn, b.rightHorn }

// RamAttacking reports if the goat lowers its head to ram, as Bedrock shows.
func (g *Goat) RamAttacking() bool { return g.behaviour().loweringHead }

// JumpGoalJumping reports if the goat is in a long jump, as Bedrock shows.
func (g *Goat) JumpGoalJumping() bool { return g.behaviour().longJumping }

// Milk ports the milking of Goat.mobInteract: adults give milk.
func (g *Goat) Milk(*world.Tx) bool { return !g.Dead() && g.behaviour().age >= 0 }

// MilkSound returns the sound of milking the goat.
func (g *Goat) MilkSound() world.Sound {
	return sound.Custom{Name: g.behaviour().sound("mob.cow.milk", "mob.goat.milk.screamer"), Volume: 1, Pitch: 1}
}

// Interact feeds the goat wheat to breed it or let a kid grow up faster.
func (g *Goat) Interact(_ *world.Tx, u item.User, c *item.UseContext) bool {
	if !feedBreeder(g, u, c) {
		return false
	}
	r := rand.Float64()*.4 + .8
	g.tx.PlaySound(g.Position(), sound.Custom{Name: "mob.goat.eat", Volume: 1, Pitch: r})
	return true
}

// InLove reports whether the goat is in love.
func (g *Goat) InLove() bool { return g.behaviour().breeding.InLove() }

func (g *Goat) breedingState() *breeding { return &g.behaviour().breeding }
func (g *Goat) ageTicks() *int           { return &g.behaviour().age }

func (g *Goat) breedingFood(stack item.Stack) bool { return itemNamed(stack, "minecraft:wheat") }

// breedOffspring ports Goat.getBreedOffspring: the kid screams like a
// randomly picked parent, or with a 2% chance regardless.
func (g *Goat) breedOffspring(_ *world.Tx, partner breeder) *world.EntityHandle {
	screaming := randomParent(g, partner.(*Goat)).behaviour().screaming || rand.Float64() < 0.02
	return babyAt(g).New(GoatType, GoatConfig{Age: babyStartAge, Screaming: screaming})
}

var _ Interactable = (*Goat)(nil)
var _ Milkable = (*Goat)(nil)

// FinalizeSpawn ports Goat.finalizeSpawn: one goat in fifty screams, and one
// adult in ten lacks a horn.
func (g *Goat) FinalizeSpawn(_ string, r *vanilla.Random) {
	if r == nil {
		return
	}
	b := g.behaviour()
	b.br.set(memLongJumpCooldownTicks, 600+int(r.NextInt(601)))
	b.br.set(memRamCooldownTicks, 600+int(r.NextInt(5401)))
	b.screaming = r.NextDouble() < .02
	if b.age >= 0 && r.NextFloat() < .1 {
		if r.NextInt(2) == 0 {
			b.leftHorn = false
		} else {
			b.rightHorn = false
		}
	}
}

var _ breeder = (*Goat)(nil)

// uuidHashCode ports UUID.hashCode.
func uuidHashCode(id [16]byte) int32 {
	var most, least uint64
	for i := range 8 {
		most = most<<8 | uint64(id[i])
		least = least<<8 | uint64(id[i+8])
	}
	hilo := most ^ least
	return int32(hilo>>32) ^ int32(hilo)
}

// walkTargetValue ports Animal.getWalkTargetValue.
func (b *GoatBehaviour) walkTargetValue(m *Mob, pos cube.Pos) float64 {
	return animalWalkTargetValue(m, pos)
}
