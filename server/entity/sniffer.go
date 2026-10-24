package entity

import (
	"slices"

	"math"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/loot"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// NewSniffer creates an adult sniffer with no active dig memory.
func NewSniffer(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(SnifferType, SnifferConfig{})
}

// SnifferType is the registered Java sniffer type.
var SnifferType snifferType

type snifferType struct{}

func (snifferType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	if d.Data == nil {
		SnifferConfig{}.Apply(d)
	}
	return &Sniffer{Ent: &Ent{tx: tx, handle: h, data: d}}
}
func (snifferType) EncodeEntity() string { return "minecraft:sniffer" }
func (snifferType) BBox(e world.Entity) cube.BBox {
	// EntityTypes.SNIFFER and Sniffer.DIGGING_DIMENSIONS, halved for
	// snifflets by AgeableMob.getAgeScale.
	w, h := 1.9, 1.75
	if s, ok := e.(*Sniffer); ok {
		b := s.behaviour()
		if b.state == snifferDigging {
			h -= .4
		}
		if b.age < 0 {
			w, h = w*.5, h*.5
		}
	}
	return cube.Box(-w/2, 0, -w/2, w/2, h, w/2)
}
func (snifferType) DecodeNBT(m map[string]any, d *world.EntityData) {
	s := ReadNativeMobJavaState(m)
	if s == nil {
		s = m
	}
	c := SnifferConfig{Age: int(nbtconv.Int32(s, "Age"))}
	if positions, ok := s["ExploredPositions"].([]int32); ok {
		for i := 0; i+2 < len(positions); i += 3 {
			c.Explored = append(c.Explored, cube.Pos{int(positions[i]), int(positions[i+1]), int(positions[i+2])})
		}
	}
	if a, ok := nativeMobAttribute(m, "minecraft:health"); ok {
		c.Health, c.MaxHealth = a.Current, a.Base
	}
	d.Data = c.New()
	d.Data.(*SnifferBehaviour).breeding.decodeNBT(s)
}
func (snifferType) EncodeNBT(d *world.EntityData) map[string]any {
	b := d.Data.(*SnifferBehaviour)
	a := EncodeNativeMobAttributes(ReadNativeMobAttributes(d.NativeNBT), []NativeMobAttribute{{Name: "minecraft:health", Base: b.maxHealth, Current: b.health, DefaultMin: 0, DefaultMax: b.maxHealth, Min: 0, Max: b.maxHealth}})
	explored := b.explored()
	positions := make([]int32, 0, len(explored)*3)
	for _, p := range explored {
		positions = append(positions, int32(p[0]), int32(p[1]), int32(p[2]))
	}
	return MergeNativeMobJavaState(map[string]any{"Attributes": a}, MergeNativeMobNBT(ReadNativeMobJavaState(d.NativeNBT), b.breeding.encodeNBT(map[string]any{"Age": int32(b.age), "ExploredPositions": positions})))
}

// SnifferConfig restores the age and explored positions of a sniffer.
type SnifferConfig struct {
	Health, MaxHealth float64
	Age               int
	Explored          []cube.Pos
}

func (c SnifferConfig) Apply(d *world.EntityData) { d.Data = c.New() }
func (c SnifferConfig) New() *SnifferBehaviour {
	if c.MaxHealth <= 0 {
		c.MaxHealth = 14
	}
	if c.Health <= 0 {
		c.Health = c.MaxHealth
	}
	b := &SnifferBehaviour{health: c.Health, maxHealth: c.MaxHealth, age: c.Age, speed: .1, effects: NewEffectManager()}
	b.br = newSnifferBrain(b)
	if len(c.Explored) > 0 {
		b.br.set(memSnifferExploredPositions, append([]cube.Pos(nil), c.Explored...))
	}
	return b
}

// snifferState is Sniffer.State.
type snifferState uint8

const (
	snifferIdling snifferState = iota
	snifferFeelingHappy
	snifferScenting
	snifferSniffing
	snifferSearching
	snifferDigging
	snifferRising
)

// Sniffer is an ancient mob that sniffs out and digs up seeds.
type Sniffer struct{ *Ent }
type SnifferBehaviour struct {
	effects           *EffectManager
	ai                MobAI
	br                *Brain
	health, maxHealth float64
	age               int
	dead              bool
	speed             float64
	breeding          breeding
	state             snifferState
	// dropSeedAt is the tick count of the MobAI the sniffer digs up a seed
	// at, Sniffer.DATA_DROP_SEED_AT_TICK.
	dropSeedAt int
}

func (s *Sniffer) behaviour() *SnifferBehaviour { return s.Behaviour().(*SnifferBehaviour) }
func (s *Sniffer) Tick(tx *world.Tx, t int64) {
	s.behaviour().effects.Tick(s, tx)
	s.Ent.Tick(tx, t)
}

func (b *SnifferBehaviour) mobAI() *MobAI   { return &b.ai }
func (b *SnifferBehaviour) brain() *Brain   { return b.br }
func (b *SnifferBehaviour) panicking() bool { return b.br.has(memIsPanicking) }

// Tick runs a tick of the sniffer: its digging, its ageing and love, and its
// MobAI with its brain.
func (b *SnifferBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	s := &Sniffer{Ent: e}
	b.ai.MovementSpeed = b.speed
	if b.ai.started {
		// Sniffer.getMaxHeadYRot.
		b.ai.MaxHeadYaw = 50
		if b.state == snifferDigging {
			// Sniffer.tick: a digging sniffer stirs the ground and digs
			// up a seed.
			m := b.ai.Mob(e, s)
			if m.AI.tickCount%10 == 0 {
				tx.EmitGameEvent(world.GameEventEntityAction, snifferHeadBlock(m).Vec3Centre(), world.GameEventContext{Source: s})
			}
			if b.dropSeedAt == m.AI.tickCount {
				b.dropSeed(m)
			}
		}
	}
	tickAgeing(s, tx)
	return b.ai.Tick(e, s, func(m *Mob) {
		m.AI.nav.SetCanFloat(true)
		m.AI.SetPathfindingMalus(PathWater, -1)
		m.AI.SetPathfindingMalus(PathOnTopOfPowderSnow, -1)
		m.AI.SetPathfindingMalus(PathDamageCautious, -1)
	})
}

// customServerAiStep ports Sniffer.customServerAiStep and
// SnifferAi.updateActivity.
func (b *SnifferBehaviour) customServerAiStep(m *Mob) {
	b.br.tick(m)
	b.br.setActiveToFirstValid(activityDig, activitySniff, activityIdle)
}

// walkTargetValue ports Animal.getWalkTargetValue.
func (b *SnifferBehaviour) walkTargetValue(m *Mob, pos cube.Pos) float64 {
	return animalWalkTargetValue(m, pos)
}

// onPathfindingStart and onPathfindingDone port those of Sniffer: a burning
// sniffer or one in water may swim to get out.
func (b *SnifferBehaviour) onPathfindingStart(m *Mob) {
	if m.OnFireDuration() > 0 || m.InWater() {
		m.AI.SetPathfindingMalus(PathWater, 0)
	}
}

func (b *SnifferBehaviour) onPathfindingDone(m *Mob) { m.AI.SetPathfindingMalus(PathWater, -1) }

// jumpFromGround ports Sniffer.jumpFromGround: a sniffer that wants to go
// somewhere and barely moves gets a push forward as it jumps.
func (b *SnifferBehaviour) jumpFromGround(m *Mob) bool {
	if power := m.AI.jumpPower(m); power > 1e-5 {
		m.data.Vel[1] = math.Max(power, m.data.Vel[1])
	}
	if m.AI.move.speedModifier > 0 && m.data.Vel[0]*m.data.Vel[0]+m.data.Vel[2]*m.data.Vel[2] < .01 {
		m.AI.moveRelative(m, .1, mgl64.Vec3{0, 0, 1})
	}
	return true
}

func (b *SnifferBehaviour) jumpInLiquid(*Mob, bool) bool { return false }

// transitionTo ports Sniffer.transitionTo.
func (b *SnifferBehaviour) transitionTo(m *Mob, state snifferState) {
	if state == snifferDigging {
		// Sniffer.onDiggingStart.
		b.dropSeedAt = m.AI.tickCount + 120
	}
	if b.state != state {
		b.state = state
		m.updateState()
	}
}

// resetSniffing ports SnifferAi.resetSniffing.
func (b *SnifferBehaviour) resetSniffing(m *Mob) {
	m.Brain().erase(memSnifferDigging)
	m.Brain().erase(memSnifferSniffingTarget)
	b.transitionTo(m, snifferIdling)
}

// tempted ports Sniffer.isTempted.
func (b *SnifferBehaviour) tempted() bool { return b.br.has(memTemptingPlayer) }

// canSniff ports Sniffer.canSniff.
func (b *SnifferBehaviour) canSniff(m *Mob) bool {
	return !b.tempted() && !b.panicking() && !m.InWater() && !b.breeding.InLove() && m.OnGround() && m.data.Riding == nil && m.Ent.HolderLeashID() == 0
}

// canDig ports Sniffer.canDig: the sniffer may dig the block in front of
// its head.
func (b *SnifferBehaviour) canDig(m *Mob) bool {
	return !b.panicking() && !b.tempted() && b.age >= 0 && !m.InWater() && m.OnGround() && m.data.Riding == nil &&
		b.canDigAt(m, snifferHeadBlock(m).Side(cube.FaceDown))
}

// canDigAt ports Sniffer.canDig(BlockPos): a block the sniffer may dig that
// it has not dug before and can walk up to.
func (b *SnifferBehaviour) canDigAt(m *Mob, pos cube.Pos) bool {
	if !blockInTag(m.tx.Block(pos), "sniffer_diggable_block") || slices.Contains(b.explored(), pos) {
		return false
	}
	path := m.AI.nav.CreatePath(m, pos, 1)
	return path != nil && path.CanReach()
}

func (b *SnifferBehaviour) explored() []cube.Pos {
	v, _ := b.br.get(memSnifferExploredPositions)
	positions, _ := v.([]cube.Pos)
	return positions
}

// storeExploredPosition ports Sniffer.storeExploredPosition: a sniffer
// remembers the last 21 blocks it dug up.
func (b *SnifferBehaviour) storeExploredPosition(pos cube.Pos) {
	old := b.explored()
	positions := make([]cube.Pos, 0, min(len(old), 20)+1)
	positions = append(positions, pos)
	positions = append(positions, old[:min(len(old), 20)]...)
	b.br.set(memSnifferExploredPositions, positions)
}

// calculateDigPosition ports Sniffer.calculateDigPosition: of five random
// spots ever further away, the first above a block the sniffer may dig.
func (b *SnifferBehaviour) calculateDigPosition(m *Mob) (cube.Pos, bool) {
	for i := range 5 {
		v, ok := LandRandomPos(m, 10+2*i, 3)
		if !ok {
			continue
		}
		pos := cube.PosFromVec3(v)
		if !m.tx.World().WithinBorder(pos) {
			continue
		}
		if below := pos.Side(cube.FaceDown); b.canDigAt(m, below) {
			return below, true
		}
	}
	return cube.Pos{}, false
}

// snifferHeadBlock ports Sniffer.getHeadBlock: the block 2.25 blocks ahead
// of the sniffer.
func snifferHeadBlock(m *Mob) cube.Pos {
	yaw := m.Yaw() * math.Pi / 180
	head := m.data.Pos.Add(mgl64.Vec3{-math.Sin(yaw) * 2.25, 0, math.Cos(yaw) * 2.25})
	return cube.PosFromVec3(mgl64.Vec3{head[0], m.data.Pos[1] + float64(float32(.2)), head[2]})
}

// dropSeed ports Sniffer.dropSeed: the sniffer digs up the loot of the
// sniffer digging table at its head.
func (b *SnifferBehaviour) dropSeed(m *Mob) {
	head := snifferHeadBlock(m)
	stacks, err := loot.Generate("minecraft:gameplay/sniffer_digging", rand.Int64(), loot.Context{Entity: "minecraft:sniffer", Origin: head.Vec3()})
	if err == nil {
		for _, stack := range stacks {
			m.tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: head.Vec3()}, stack))
		}
	}
	m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "mob.sniffer.drop_seed", Volume: 1, Pitch: 1})
}

// newSnifferBrain ports SnifferAi and the brain provider of Sniffer.
func newSnifferBrain(b *SnifferBehaviour) *Brain {
	br := newBrain(nearestLivingEntitySensor{}, hurtBySensor{}, playerSensor{}, temptingSensor{items: animalTemptation})
	br.register(memSnifferExploredPositions)
	resetFirst := func(bh *behavior) *behavior {
		start := bh.start
		bh.start = func(m *Mob, time int64) {
			b.resetSniffing(m)
			start(m, time)
		}
		return bh
	}
	br.addActivity(activityCore, 0,
		newSwim(.8),
		newAnimalPanic(2, nil, nil, b.resetSniffing),
		newMoveToTargetSink(500, 700, nil),
		newCountDownCooldownTicks(memTemptationCooldownTicks),
	)
	br.addActivityWith(activitySniff, []prioritizedBehavior{{0, newSnifferSearching(b)}},
		[]memoryCondition{{memIsPanicking, memAbsent}, {memSnifferSniffingTarget, memPresent}, {memWalkTarget, memPresent}}, nil)
	br.addActivityWith(activityDig, []prioritizedBehavior{{0, newSnifferDigging(b, 160, 180)}, {0, newSnifferFinishedDigging(b, 40)}},
		[]memoryCondition{{memIsPanicking, memAbsent}, {memWalkTarget, memAbsent}, {memSnifferDigging, memPresent}}, nil)
	br.addActivityWith(activityIdle, []prioritizedBehavior{
		{0, resetFirst(newAnimalMakeLove(SnifferType, 1, 2))},
		{1, resetFirst(newFollowTemptation(constSpeed(1.25), func(*Mob) float64 {
			if b.age < 0 {
				return 2.5
			}
			return 3.5
		}, false))},
		{2, newLookAtTargetSink(45, 90)},
		{3, newSnifferFeelingHappy(b, 40, 100)},
		{4, newRunOne(nil,
			weightedBehavior{newSetWalkTargetFromLookTarget(nil, constSpeed(1), 3), 2},
			weightedBehavior{newSnifferScenting(b, 40, 80), 1},
			weightedBehavior{newSnifferSniffing(b, 40, 80), 1},
			weightedBehavior{newSetEntityLookTarget(isPlayerType, 6), 1},
			weightedBehavior{newRandomStroll(1, true), 1},
			weightedBehavior{newDoNothing(5, 20), 2},
		)},
	}, []memoryCondition{{memSnifferDigging, memAbsent}}, nil)
	return br
}

// newSnifferDigging ports SnifferAi.Digging: a sniffer digs for eight or
// nine seconds, after which it may not sniff for eight minutes.
func newSnifferDigging(b *SnifferBehaviour, minDuration, maxDuration int) *behavior {
	bh := &behavior{
		entry: []memoryCondition{{memIsPanicking, memAbsent}, {memWalkTarget, memAbsent}, {memSnifferDigging, memPresent},
			{memSniffCooldown, memAbsent}},
		minDuration: minDuration, maxDuration: maxDuration,
		checkExtra: b.canSniff,
		canStillUse: func(m *Mob, _ int64) bool {
			return m.Brain().has(memSnifferDigging) && b.canDig(m) && !b.breeding.InLove()
		},
		start: func(m *Mob, _ int64) { b.transitionTo(m, snifferDigging) },
	}
	bh.stop = func(m *Mob, time int64) {
		if time > bh.endTimestamp {
			m.Brain().setWithExpiry(memSniffCooldown, true, 9600)
		} else {
			b.resetSniffing(m)
		}
	}
	return bh
}

// newSnifferFinishedDigging ports SnifferAi.FinishedDigging: a sniffer done
// digging rises for two seconds and is glad of it.
func newSnifferFinishedDigging(b *SnifferBehaviour, duration int) *behavior {
	bh := &behavior{
		entry: []memoryCondition{{memIsPanicking, memAbsent}, {memWalkTarget, memAbsent}, {memSnifferDigging, memPresent},
			{memSniffCooldown, memPresent}},
		minDuration: duration, maxDuration: duration,
		canStillUse: func(m *Mob, _ int64) bool { return m.Brain().has(memSnifferDigging) },
		start:       func(m *Mob, _ int64) { b.transitionTo(m, snifferRising) },
	}
	bh.stop = func(m *Mob, time int64) {
		b.transitionTo(m, snifferIdling)
		if time > bh.endTimestamp {
			// Sniffer.onDiggingComplete.
			b.storeExploredPosition(m.BlockPos().Side(cube.FaceDown))
		}
		m.Brain().erase(memSnifferDigging)
		m.Brain().set(memSnifferHappy, true)
	}
	return bh
}

// newSnifferFeelingHappy ports SnifferAi.FeelingHappy.
func newSnifferFeelingHappy(b *SnifferBehaviour, minDuration, maxDuration int) *behavior {
	return &behavior{
		entry:       []memoryCondition{{memSnifferHappy, memPresent}},
		minDuration: minDuration, maxDuration: maxDuration,
		canStillUse: func(*Mob, int64) bool { return true },
		start:       func(m *Mob, _ int64) { b.transitionTo(m, snifferFeelingHappy) },
		stop: func(m *Mob, _ int64) {
			b.transitionTo(m, snifferIdling)
			m.Brain().erase(memSnifferHappy)
		},
	}
}

// newSnifferScenting ports SnifferAi.Scenting.
func newSnifferScenting(b *SnifferBehaviour, minDuration, maxDuration int) *behavior {
	return &behavior{
		entry: []memoryCondition{{memIsPanicking, memAbsent}, {memSnifferDigging, memAbsent}, {memSnifferSniffingTarget, memAbsent},
			{memSnifferHappy, memAbsent}, {memBreedTarget, memAbsent}},
		minDuration: minDuration, maxDuration: maxDuration,
		checkExtra:  func(*Mob) bool { return !b.tempted() },
		canStillUse: func(*Mob, int64) bool { return true },
		start:       func(m *Mob, _ int64) { b.transitionTo(m, snifferScenting) },
		stop:        func(m *Mob, _ int64) { b.transitionTo(m, snifferIdling) },
	}
}

// newSnifferSniffing ports SnifferAi.Sniffing: an adult sniffer that sniffs
// for its full time picks a block to dig up and walks to it.
func newSnifferSniffing(b *SnifferBehaviour, minDuration, maxDuration int) *behavior {
	bh := &behavior{
		entry:       []memoryCondition{{memWalkTarget, memAbsent}, {memSnifferSniffingTarget, memAbsent}, {memSniffCooldown, memAbsent}},
		minDuration: minDuration, maxDuration: maxDuration,
		checkExtra:  func(m *Mob) bool { return b.age >= 0 && b.canSniff(m) },
		canStillUse: func(m *Mob, _ int64) bool { return b.canSniff(m) },
		start:       func(m *Mob, _ int64) { b.transitionTo(m, snifferSniffing) },
	}
	bh.stop = func(m *Mob, time int64) {
		b.transitionTo(m, snifferIdling)
		if time <= bh.endTimestamp {
			return
		}
		if pos, ok := b.calculateDigPosition(m); ok {
			m.Brain().set(memSnifferSniffingTarget, pos)
			m.Brain().set(memWalkTarget, walkTarget{target: newBlockPosTracker(pos), speed: 1.25})
		}
	}
	return bh
}

// newSnifferSearching ports SnifferAi.Searching: a sniffer walks to the
// block it sniffed out and, once there, digs it up.
func newSnifferSearching(b *SnifferBehaviour) *behavior {
	return &behavior{
		entry:       []memoryCondition{{memWalkTarget, memPresent}, {memIsPanicking, memAbsent}, {memSnifferSniffingTarget, memPresent}},
		minDuration: 600, maxDuration: 600,
		checkExtra: b.canSniff,
		canStillUse: func(m *Mob, _ int64) bool {
			if !b.canSniff(m) {
				b.transitionTo(m, snifferIdling)
				return false
			}
			br := m.Brain()
			wv, ok := br.get(memWalkTarget)
			tv, ok2 := br.get(memSnifferSniffingTarget)
			return ok && ok2 && wv.(walkTarget).target.currentBlockPos(m.tx) == tv.(cube.Pos)
		},
		start: func(m *Mob, _ int64) { b.transitionTo(m, snifferSearching) },
		stop: func(m *Mob, _ int64) {
			if b.canDig(m) && b.canSniff(m) {
				m.Brain().set(memSnifferDigging, true)
			}
			m.Brain().erase(memWalkTarget)
			m.Brain().erase(memSnifferSniffingTarget)
		},
	}
}

func (s *Sniffer) Health() float64    { return s.behaviour().health }
func (s *Sniffer) MaxHealth() float64 { return s.behaviour().maxHealth }
func (s *Sniffer) SetMaxHealth(v float64) {
	b := s.behaviour()
	b.maxHealth = v
	b.health = math.Min(v, b.health)
}
func (s *Sniffer) Dead() bool { return s.behaviour().dead || s.Health() <= 0 }

// Panicking reports whether the sniffer runs from danger.
func (s *Sniffer) Panicking() bool { return s.behaviour().panicking() }
func (s *Sniffer) Hurt(v float64, src world.DamageSource) (float64, bool) {
	if s.Dead() || v <= 0 {
		return 0, false
	}
	b := s.behaviour()
	var ok bool
	if v, ok = b.ai.DamageCooldown(s.tx, v); !ok {
		return 0, false
	}
	if v, ok = mobEffectDamage(b.effects, v, src); !ok {
		return 0, false
	}
	b.health -= v
	b.ai.Hurt(src)
	resetLove(s)
	s.viewAction(HurtAction{})
	onMobHurt(s.tx, s)
	if s.Dead() {
		b.dead = true
		// Sniffer.die.
		b.state = snifferIdling
		s.viewAction(DeathAction{})
		dropAllDeathLoot(s.tx, s, src)
		_ = s.CloseIn(s.tx)
	}
	return v, true
}
func (s *Sniffer) Heal(v float64, _ world.HealingSource) float64 {
	if s.Dead() || v <= 0 {
		return 0
	}
	b := s.behaviour()
	old := b.health
	b.health = math.Min(b.maxHealth, b.health+v)
	return b.health - old
}
func (s *Sniffer) KnockBack(src mgl64.Vec3, f, h float64) {
	d := s.Position().Sub(src)
	d[1] = 0
	if d.Len() > 0 {
		d = d.Normalize().Mul(f)
	}
	d[1] = h
	s.SetVelocity(d)
}
func (s *Sniffer) viewAction(a world.EntityAction) {
	vs := s.tx.Viewers(s.Position())
	for _, v := range vs {
		v.ViewEntityAction(s, a)
	}
	s.tx.ReleaseViewers(vs)
}

// SnifferState returns the flag Bedrock shows for the state of the sniffer:
// feeling happy, scenting, sniffing, searching, digging or rising, or none
// while it idles.
func (s *Sniffer) SnifferState() string {
	return [...]string{"", "feeling_happy", "scenting", "sniffing", "searching", "digging", "rising"}[s.behaviour().state]
}

// Sniffing reports whether the sniffer sniffs the air.
func (s *Sniffer) Sniffing() bool { return s.behaviour().state == snifferSniffing }

// Digging reports whether the sniffer digs.
func (s *Sniffer) Digging() bool { return s.behaviour().state == snifferDigging }

// SetAge sets the age in ticks; negative ages are babies growing up.
func (s *Sniffer) SetAge(age int) { s.behaviour().age = age }

// Interact feeds the sniffer torchflower seeds to breed it or let a snifflet
// grow up faster.
func (s *Sniffer) Interact(tx *world.Tx, u item.User, c *item.UseContext) bool {
	if !feedBreeder(s, u, c) {
		return false
	}
	// Sniffer.playEatingSound.
	tx.PlaySound(s.Position(), sound.Custom{Name: "mob.sniffer.eat", Volume: 1, Pitch: .8 + rand.Float64()*.4})
	return true
}

// canMateWith ports Sniffer.canMate: both sniffers idle, scent or are glad.
func (s *Sniffer) canMateWith(partner breeder) bool {
	other, ok := partner.(*Sniffer)
	if !ok {
		return false
	}
	calm := func(st snifferState) bool {
		return st == snifferIdling || st == snifferScenting || st == snifferFeelingHappy
	}
	return calm(s.behaviour().state) && calm(other.behaviour().state)
}

// InLove reports whether the sniffer is in love.
func (s *Sniffer) InLove() bool { return s.behaviour().breeding.InLove() }

func (s *Sniffer) breedingState() *breeding { return &s.behaviour().breeding }
func (s *Sniffer) ageTicks() *int           { return &s.behaviour().age }

func (s *Sniffer) breedingFood(stack item.Stack) bool { return inItemTag(stack, "sniffer_food") }

func (s *Sniffer) breedOffspring(_ *world.Tx, _ breeder) *world.EntityHandle {
	return babyAt(s).New(SnifferType, SnifferConfig{Age: babyStartAge})
}

// spawnChildFromBreeding ports Sniffer.spawnChildFromBreeding: instead of a
// snifflet, the sniffer lays a sniffer egg item with a plop.
func (s *Sniffer) spawnChildFromBreeding(tx *world.Tx, partner breeder) {
	finishBreeding(s, partner, tx)
	tx.PlaySound(s.Position(), sound.NativeEvent{SoundType: "plop", EntityType: "minecraft:sniffer", ExtraData: -1})
	tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: s.Position()}, item.NewStack(block.SnifferEgg{}, 1)))
}

var _ Interactable = (*Sniffer)(nil)
