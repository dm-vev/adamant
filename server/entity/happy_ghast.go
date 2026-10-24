package entity

import (
	"math"

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

// HappyGhastType is the source happy ghast species. Babies use the same native
// actor identity; age and all lifecycle state are persisted as Java state.
var HappyGhastType world.EntityType = happyGhastType{}

type happyGhastType struct{}

func (happyGhastType) EncodeEntity() string { return "minecraft:happy_ghast" }
func (happyGhastType) BBox(e world.Entity) cube.BBox {
	baby := false
	if h, ok := e.(*HappyGhast); ok {
		baby = h.state().age < 0
	}
	w, high := 4.0, 4.0
	if baby {
		w, high = .95, .95
	}
	return cube.Box(-w/2, 0, -w/2, w/2, high, w/2)
}
func (happyGhastType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	if d.Data == nil {
		happyGhastConfig{}.Apply(d)
	}
	return &HappyGhast{Ent: &Ent{tx: tx, handle: h, data: d}}
}
func (happyGhastType) DecodeNBT(n map[string]any, d *world.EntityData) {
	c := happyGhastConfig{}
	s := ReadNativeMobJavaState(n)
	if s == nil {
		s = n
	}
	c.age = int(nbtconv.Int32(s, "Age"))
	c.harness = nbtconv.Bool(s, "Harnessed")
	c.harnessName = nbtconv.String(s, "HarnessName")
	if c.harness && c.harnessName == "" {
		c.harnessName = "minecraft:white_harness"
	}
	c.still = int(nbtconv.Int32(s, "StillTimeout"))
	if nbtconv.Bool(s, "HasHome") {
		c.home = mobHome{pos: cube.Pos{int(nbtconv.Int32(s, "HomeX")), int(nbtconv.Int32(s, "HomeY")), int(nbtconv.Int32(s, "HomeZ"))}, radius: int(nbtconv.Int32(s, "HomeRadius"))}
		if c.home.radius <= 0 {
			c.home.radius = happyGhastLargeRadius
		}
	}
	c.breeding.decodeNBT(s)
	if _, ok := s["Health"]; ok {
		c.health = float64(nbtconv.Float32(s, "Health"))
	} else if a, ok := nativeMobAttribute(n, "minecraft:health"); ok {
		c.health = a.Current
	}
	c.Apply(d)
}
func (happyGhastType) EncodeNBT(d *world.EntityData) map[string]any {
	b := d.Data.(*happyGhastBehaviour)
	h := b.health
	attrs := EncodeNativeMobAttributes(ReadNativeMobAttributes(d.NativeNBT), []NativeMobAttribute{{Name: "minecraft:health", Base: 20, Current: h.Health(), DefaultMin: 0, DefaultMax: 20, Min: 0, Max: 20}, {Name: "minecraft:movement", Base: .05, Current: .05, DefaultMin: 0, DefaultMax: .05, Min: 0, Max: .05}})
	m := MergeNativeMobNBT(d.NativeNBT, map[string]any{"Attributes": attrs})
	home := b.currentHome()
	state := map[string]any{"Age": int32(b.age), "Harnessed": boolByte(b.harness), "HarnessName": b.harnessName, "StillTimeout": int32(b.still), "HasHome": boolByte(home.set()), "Health": float32(h.Health())}
	if home.set() {
		state["HomeX"], state["HomeY"], state["HomeZ"], state["HomeRadius"] = int32(home.pos[0]), int32(home.pos[1]), int32(home.pos[2]), int32(home.radius)
	}
	return MergeNativeMobJavaState(m, b.breeding.encodeNBT(state))
}

type happyGhastConfig struct {
	age, still  int
	harness     bool
	harnessName string
	home        mobHome
	breeding    breeding
	health      float64
}

func (c happyGhastConfig) Apply(d *world.EntityData) { d.Data = c.New() }
func (c happyGhastConfig) New() *happyGhastBehaviour {
	if c.health <= 0 {
		c.health = 20
	}
	b := &happyGhastBehaviour{health: NewHealthManager(c.health, 20), effects: NewEffectManager(), speed: .05, age: c.age, still: c.still, harness: c.harness, harnessName: c.harnessName, home: c.home, breeding: c.breeding}
	b.ghastMove = ghastMoveControl{careful: true, stopped: func(*Mob) bool { return b.stillTimeout() }}
	b.br = newHappyGhastBrain()
	return b
}

// The home radius of a happy ghast: small while harnessed or a baby, large
// otherwise, with a buffer it may leave its home by before it moves it.
const (
	happyGhastSmallRadius  = 32
	happyGhastLargeRadius  = 64
	happyGhastRadiusBuffer = 16
)

type happyGhastBehaviour struct {
	health      *HealthManager
	effects     *EffectManager
	ai          MobAI
	br          *Brain
	ghastMove   ghastMoveControl
	speed       float64
	age         int
	breeding    breeding
	harness     bool
	harnessName string
	riders      [4]*world.EntityHandle
	// strafe, forward and jumping are the input of the player steering the
	// happy ghast.
	strafe, forward float64
	jumping         bool
	// still is HappyGhast.serverStillTimeout: the ticks the happy ghast
	// stays in place after a player stood on it or got off.
	still int
	// home is the home read from NBT until the AI starts.
	home mobHome
	// babySetup is whether the controls of a baby were set up last.
	babySetup bool
}
type HappyGhast struct{ *Ent }

func NewHappyGhast(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(HappyGhastType, happyGhastConfig{})
}
func (*HappyGhast) SpeciesType() world.EntityType { return HappyGhastType }

// FinalizeSpawn initializes the source-created ghastling as a baby before it is exposed to viewers.
func (h *HappyGhast) FinalizeSpawn(reason string, _ *vanilla.Random) {
	if reason == "breeding" {
		h.state().age = babyStartAge
	}
}
func (h *HappyGhast) state() *happyGhastBehaviour { return h.Behaviour().(*happyGhastBehaviour) }
func (h *HappyGhast) Tick(tx *world.Tx, n int64)  { h.state().effects.Tick(h, tx); h.Ent.Tick(tx, n) }

func (b *happyGhastBehaviour) mobAI() *MobAI   { return &b.ai }
func (b *happyGhastBehaviour) brain() *Brain   { return b.br }
func (b *happyGhastBehaviour) panicking() bool { return b.br.has(memIsPanicking) }

// currentHome returns the home of the happy ghast.
func (b *happyGhastBehaviour) currentHome() mobHome {
	if b.ai.started {
		return b.ai.home
	}
	return b.home
}

// Tick runs the MobAI of the happy ghast, then HappyGhast.tick: a player
// standing on it keeps it in place.
func (b *happyGhastBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	h := &HappyGhast{Ent: e}
	// HappyGhast.createAttributes.
	b.ai.MovementSpeed, b.ai.FlyingSpeed, b.ai.FollowRange = b.speed, .05, 16
	b.ai.EyeHeight = 2.6
	if b.age < 0 {
		b.ai.EyeHeight = .46875
	}
	if b.harness {
		b.restoreRiders(e, tx)
	}
	tickAgeing(h, tx)
	if b.ai.started && b.babySetup != (b.age < 0) {
		// AgeableMob.ageBoundaryReached.
		b.setup(b.ai.Mob(e, h))
	}
	mov := b.ai.Tick(e, h, func(m *Mob) {
		m.AI.home = b.home
		if !b.home.set() {
			m.AI.home = noHome
		}
		m.AI.nav.SetFlying()
		m.AI.nav.SetCanOpenDoors(false)
		m.AI.nav.SetCanFloat(true)
		m.AI.nav.SetRequiredPathLength(m, 48)
		m.AI.nav.SetFlyThroughLiquids()
		b.setup(m)
	})
	if b.still > 0 {
		if b.ai.tickCount > 60 {
			b.still--
		}
		b.setStill(h, b.still)
	}
	if b.playerAbove(e, tx) {
		b.setStill(h, 10)
	}
	b.updateRiders(e, tx)
	return mov
}

// setup ports HappyGhast.babyGhastSetup and adultGhastSetup: a ghastling
// flies about with its brain, and a grown happy ghast floats with goals.
func (b *happyGhastBehaviour) setup(m *Mob) {
	baby := b.age < 0
	b.babySetup = baby
	ai := m.AI
	ai.Goals.Remove(m, func(Goal) bool { return true })
	if baby {
		ai.moveOverride = flyingMoveControl{maxTurn: 180, hoversInPlace: true}
		ai.lookOverride = nil
		b.setStill(m.Self.(*HappyGhast), 0)
		return
	}
	ai.moveOverride = &b.ghastMove
	ai.lookOverride = happyGhastLookControl{b}
	ai.nav.Stop()
	b.registerGoals(m)
	b.br.stopAll(m)
	b.br.clearMemories()
}

// registerGoals ports HappyGhast.registerGoals.
func (b *happyGhastBehaviour) registerGoals(m *Mob) {
	ai := m.AI
	ai.Goals.Add(3, &happyGhastFloatGoal{FloatGoal: NewFloatGoal(m), b: b})
	tempt := NewTemptGoal(1, func(s item.Stack) bool {
		if !b.harness && b.age >= 0 {
			return inItemTag(s, "happy_ghast_tempt_items")
		}
		return inItemTag(s, "happy_ghast_food")
	}, false)
	tempt.StopDistance, tempt.Range = 7, 16
	// TemptGoal.ForNonPathfinders: the happy ghast floats straight towards
	// a random point between it and the eyes of the player.
	tempt.StopNavigation = func(m *Mob) { m.AI.move.SetWait() }
	tempt.NavigateTowards = func(m *Mob, p world.Entity) {
		eye := mgl64.Vec3{p.Position()[0], entityEyeY(p), p.Position()[2]}
		target := eye.Sub(m.data.Pos).Mul(m.Random().NextDouble()).Add(m.data.Pos)
		m.AI.move.SetWantedPosition(target[0], target[1], target[2], tempt.SpeedModifier)
	}
	ai.Goals.Add(4, tempt)
	ai.Goals.Add(5, &ghastRandomFloatAroundGoal{goalFlags: goalFlags{GoalMove}, distanceToBlocks: 16})
}

// newHappyGhastBrain ports HappyGhastAi and the brain provider of
// HappyGhast, which only ghastlings use.
func newHappyGhastBrain() *Brain {
	br := newBrain(nearestLivingEntitySensor{}, hurtBySensor{}, temptingSensor{items: animalTemptation, rangeLimit: 16}, adultSensor{anyType: true}, playerSensor{})
	br.addActivity(activityCore, 0,
		newSwim(.8),
		newAnimalPanic(2, nil, func(m *Mob) (mgl64.Vec3, bool) {
			dir := m.data.Rot.Vec3()
			return AirAndWaterRandomPos(m, 5, 4, 0, dir[0], dir[2], math.Pi/2)
		}, nil),
		newLookAtTargetSink(45, 90),
		newMoveToTargetSink(150, 250, nil),
		newCountDownCooldownTicks(memTemptationCooldownTicks),
	)
	followRange := uniformInt{3, 16}
	br.addActivityWith(activityIdle, []prioritizedBehavior{
		{1, newFollowTemptation(constSpeed(1.25), func(*Mob) float64 { return 3 }, true)},
		{2, newBabyFollowAdult(followRange, constSpeed(1.1), memNearestVisiblePlayer, true)},
		{3, newBabyFollowAdult(followRange, constSpeed(1.1), memNearestVisibleAdult, true)},
		{4, newRunOne(nil,
			weightedBehavior{newRandomFly(1), 1},
			weightedBehavior{newSetWalkTargetFromLookTarget(nil, constSpeed(1), 3), 1},
		)},
	}, nil, nil)
	br.addActivityWith(activityPanic, nil, []memoryCondition{{memIsPanicking, memPresent}}, nil)
	return br
}

// customServerAiStep ports HappyGhast.customServerAiStep: a ghastling
// thinks with its brain, and every happy ghast keeps its home near.
func (b *happyGhastBehaviour) customServerAiStep(m *Mob) {
	if b.age < 0 {
		b.br.tick(m)
		b.br.setActiveToFirstValid(activityPanic, activityIdle)
	}
	b.checkRestriction(m)
}

// checkRestriction ports HappyGhast.checkRestriction: a happy ghast nobody
// rides moves its home to where it is once it strays too far from it.
func (b *happyGhastBehaviour) checkRestriction(m *Mob) {
	if b.hasRiders() {
		return
	}
	radius := happyGhastSmallRadius
	if b.age >= 0 && !b.harness {
		radius = happyGhastLargeRadius
	}
	home, pos := m.AI.home, m.BlockPos()
	limit := radius + happyGhastRadiusBuffer
	if !home.set() || distSqrPos(home.pos, pos) >= float64(limit*limit) || radius != home.radius {
		m.SetHome(pos, radius)
	}
}

// afterAiStep ports HappyGhast.continuousHeal: a hurt happy ghast heals a
// point every second in rain or clouds, and every thirty seconds otherwise.
func (b *happyGhastBehaviour) afterAiStep(m *Mob) {
	h := m.Self.(*HappyGhast)
	if h.Dead() || h.Health() == h.MaxHealth() {
		return
	}
	interval := 600
	pos := m.BlockPos()
	if happyGhastInClouds(m) || m.tx.RainingAt(pos) || m.tx.SnowingAt(pos) {
		interval = 20
	}
	if m.AI.tickCount%interval == 0 {
		h.Heal(1, nil)
	}
}

// happyGhastInClouds ports Entity.isInClouds: the overworld has clouds four
// blocks thick at a height of 192.33.
func happyGhastInClouds(m *Mob) bool {
	if m.tx.World().Dimension() != world.Overworld {
		return false
	}
	const cloudBottom = float32(192.33)
	y := m.data.Pos[1]
	return y+m.Height() >= float64(cloudBottom) && y <= float64(cloudBottom+4)
}

// travel ports HappyGhast.travel and LivingEntity.travelRidden: a happy
// ghast flies, steered by the player riding it first.
func (b *happyGhastBehaviour) travel(m *Mob, input mgl64.Vec3) bool {
	if c := b.controller(m.tx); c != nil && m.Alive() {
		input = b.riddenInput(m, c)
		b.tickRidden(m, c)
	}
	speed := float64(float32(m.AI.FlyingSpeed) * 5 / 3)
	m.AI.travelFlying(m, input, speed, speed, speed)
	return true
}

// steeredByPlayer reports if a player steers the happy ghast, which then
// leaves its AI to the player.
func (b *happyGhastBehaviour) steeredByPlayer(m *Mob) bool { return b.controller(m.tx) != nil }

// controller ports HappyGhast.getControllingPassenger: the player riding a
// harnessed happy ghast first steers it unless it stays in place.
func (b *happyGhastBehaviour) controller(tx *world.Tx) world.Entity {
	if !b.harness || b.stillTimeout() || b.riders[0] == nil {
		return nil
	}
	e, ok := b.riders[0].Entity(tx)
	if !ok || !isPlayerEntity(e) {
		return nil
	}
	return e
}

// riddenInput ports HappyGhast.getRiddenInput: the happy ghast flies the way
// its rider looks while the rider moves forward, half as fast backwards,
// and up while the rider jumps.
func (b *happyGhastBehaviour) riddenInput(m *Mob, c world.Entity) mgl64.Vec3 {
	// LivingEntity.aiStep slows the input of the rider.
	strafe, zza := b.strafe*.98, b.forward*.98
	var up, forward float64
	if zza != 0 {
		pitch := c.Rotation().Pitch() * math.Pi / 180
		forward, up = math.Cos(pitch), -math.Sin(pitch)
		if zza < 0 {
			forward, up = forward*-.5, up*-.5
		}
	}
	if b.jumping {
		up += .5
	}
	return mgl64.Vec3{strafe, up, forward}.Mul(float64(float32(3.9)) * m.AI.FlyingSpeed)
}

// tickRidden ports HappyGhast.tickRidden: the happy ghast slowly turns the
// way its rider looks.
func (b *happyGhastBehaviour) tickRidden(m *Mob, c world.Entity) {
	rot := c.Rotation()
	yaw := m.Yaw()
	yaw += wrapDegrees(rot.Yaw()-yaw) * .08
	m.data.Rot = cube.Rotation{yaw, rot.Pitch() * .5}
	m.AI.headYaw, m.AI.bodyYaw = yaw, yaw
}

// walkTargetValue ports HappyGhast.getWalkTargetValue: a ghastling prefers
// to fly a block above the ground.
func (b *happyGhastBehaviour) walkTargetValue(m *Mob, pos cube.Pos) float64 {
	empty := func(pos cube.Pos) bool {
		_, air := pathBlockAt(m.tx, pos).(block.Air)
		return air
	}
	switch {
	case !empty(pos):
		return 0
	case empty(pos.Side(cube.FaceDown)) && !empty(pos.Sub(cube.Pos{0, 2, 0})):
		return 10
	}
	return 5
}

// stillTimeout ports HappyGhast.isOnStillTimeout.
func (b *happyGhastBehaviour) stillTimeout() bool { return b.still > 0 }

// setStill ports HappyGhast.setServerStillTimeout: viewers learn whether
// the happy ghast may move.
func (b *happyGhastBehaviour) setStill(h *HappyGhast, still int) {
	was := b.stillTimeout()
	b.still = still
	if was != b.stillTimeout() && h.tx != nil {
		h.updateState()
	}
}

// playerAbove ports HappyGhast.scanPlayerAboveGhast: a player, or the
// vehicle of one that is not a happy ghast, on top of the happy ghast.
func (b *happyGhastBehaviour) playerAbove(e *Ent, tx *world.Tx) bool {
	bb := e.H().Type().BBox(e).Translate(e.data.Pos)
	lo, hi := bb.Min(), bb.Max()
	minY := hi[1] - float64(float32(1e-5))
	maxY := hi[1] + bb.Height()/2
	for p := range tx.Players() {
		if spectator(p) {
			continue
		}
		root := world.Entity(p)
		if r, ok := p.(Rider); ok && r.Riding() != nil {
			v, ok := r.Riding().Entity(tx)
			if !ok {
				continue
			}
			if _, ghast := v.(*HappyGhast); ghast {
				continue
			}
			root = v
		}
		pos := root.Position()
		if pos[0] >= lo[0]-1 && pos[0] < hi[0]+1 && pos[1] >= minY && pos[1] < maxY && pos[2] >= lo[2]-1 && pos[2] < hi[2]+1 {
			return true
		}
	}
	return false
}

// StaysStill reports if the happy ghast stays in place, as HappyGhast.staysStill.
func (h *HappyGhast) StaysStill() bool { return h.state().stillTimeout() }

// happyGhastLookControl ports HappyGhast.HappyGhastLookControl: a happy
// ghast that stays in place squares up to the nearest quarter turn, and
// otherwise faces what it looks at or the way it moves.
type happyGhastLookControl struct{ b *happyGhastBehaviour }

func (c happyGhastLookControl) tick(m *Mob) {
	look := &m.AI.look
	switch {
	case c.b.stillTimeout():
		yaw := m.Yaw()
		yaw -= wrapDegrees90(yaw)
		m.SetYaw(yaw)
		m.AI.headYaw = yaw
	case look.cooldown > 0:
		look.cooldown--
		yaw := -math.Atan2(look.wanted[0]-m.data.Pos[0], look.wanted[2]-m.data.Pos[2]) * 180 / math.Pi
		m.SetYaw(yaw)
		m.AI.bodyYaw, m.AI.headYaw = yaw, yaw
	default:
		ghastFaceMovement(m)
	}
}

// happyGhastFloatGoal ports HappyGhast.HappyGhastFloatGoal: a happy ghast
// swims up unless it stays in place.
type happyGhastFloatGoal struct {
	*FloatGoal
	b *happyGhastBehaviour
}

func (g *happyGhastFloatGoal) CanUse(m *Mob) bool {
	return !g.b.stillTimeout() && g.FloatGoal.CanUse(m)
}

func (b *happyGhastBehaviour) hasRiders() bool {
	for _, h := range b.riders {
		if h != nil {
			return true
		}
	}
	return false
}
func (b *happyGhastBehaviour) restoreRiders(e *Ent, tx *world.Tx) {
	if b.hasRiders() {
		return
	}
	box := cube.Box(-9, 0, -9, 9, 6, 9).Translate(e.Position())
	for rider := range tx.EntitiesWithin(box) {
		r, ok := rider.(Rider)
		if !ok || r.Riding() != e.H() {
			continue
		}
		for i := range b.riders {
			if b.riders[i] == nil {
				b.riders[i] = rider.H()
				break
			}
		}
	}
}
func (b *happyGhastBehaviour) updateRiders(e *Ent, tx *world.Tx) {
	for i, h := range b.riders {
		if h == nil {
			continue
		}
		p, ok := h.Entity(tx)
		if !ok {
			b.riders[i] = nil
			continue
		}
		r, ok := p.(Rider)
		if !ok || r.Riding() != e.H() {
			b.riders[i] = nil
			continue
		}
		angle := float64(i) * math.Pi / 2
		off := mgl64.Vec3{-math.Sin(angle) * 1.7, 4, math.Cos(angle) * 1.7}
		if t, ok := p.(interface{ Teleport(mgl64.Vec3) }); ok {
			t.Teleport(e.Position().Add(off))
		}
	}
}

// SetVehicleInputFrom sets the movement input of the player steering the
// happy ghast.
func (h *HappyGhast) SetVehicleInputFrom(passenger world.Entity, strafe, forward float64) {
	if passenger == nil || !h.ControlsVehicle(passenger) {
		strafe, forward = 0, 0
	}
	h.SetVehicleInput(strafe, forward)
}
func (h *HappyGhast) SetVehicleInput(strafe, forward float64) {
	b := h.state()
	b.strafe, b.forward = strafe, forward
}

// SetVehicleJumping sets whether the player steering the happy ghast jumps,
// which flies it up.
func (h *HappyGhast) SetVehicleJumping(jumping bool) { h.state().jumping = jumping }

// RideJump reports whether the happy ghast takes the jump of its rider,
// which flies it up instead of letting the player get off.
func (h *HappyGhast) RideJump() bool { return h.state().controller(h.tx) != nil }

func (h *HappyGhast) ControlsVehicle(passenger world.Entity) bool {
	b := h.state()
	return passenger != nil && b.harness && b.still == 0 && b.riders[0] != nil && b.riders[0] == passenger.H()
}

// Mount ports HappyGhast.addPassenger: the goggles of the harness come down
// for the first rider, and a happy ghast nobody stands on may move again.
func (h *HappyGhast) Mount(tx *world.Tx, p world.Entity) bool {
	if p == nil {
		return false
	}
	r, ok := p.(Rider)
	if !ok || r.Riding() != nil {
		return false
	}
	b := h.state()
	if !b.harness || b.age < 0 {
		return false
	}
	for i, x := range b.riders {
		if x == nil {
			if !b.hasRiders() {
				tx.PlaySound(h.Position(), sound.NativeEvent{SoundType: "attach", EntityType: "minecraft:happy_ghast", ExtraData: -1})
			}
			b.riders[i] = p.H()
			if !b.playerAbove(h.Ent, tx) {
				b.setStill(h, 0)
			} else if b.still > 10 {
				b.setStill(h, 10)
			}
			seatRider(r, h)
			h.viewLink(tx, p.H(), true)
			return true
		}
	}
	return false
}

// Dismount ports HappyGhast.removePassenger: the happy ghast stays in place
// for half a second, and once nobody rides it, it forgets its home and the
// goggles go up.
func (h *HappyGhast) Dismount(tx *world.Tx, p world.Entity) {
	if p == nil {
		return
	}
	b := h.state()
	for i, x := range b.riders {
		if x == p.H() {
			b.riders[i] = nil
			if r, ok := p.(Rider); ok {
				r.SetRiding(nil)
			}
			h.viewLink(tx, x, false)
			b.setStill(h, 10)
			if !b.hasRiders() {
				b.ai.home, b.home = noHome, noHome
				tx.PlaySound(h.Position(), sound.NativeEvent{SoundType: "detach", EntityType: "minecraft:happy_ghast", ExtraData: -1})
			}
		}
	}
}

// happyGhastSeats are the seats of Bedrock's harnessed happy ghast: riders
// fill them front, left, back and right.
var happyGhastSeats = [len(happyGhastBehaviour{}.riders)]mgl64.Vec3{{0, 3.8, 1.7}, {-1.7, 3.8, 0}, {0, 3.8, -1.7}, {1.7, 3.8, 0}}

// RiderSeat returns the seat rider fills on the harness.
func (h *HappyGhast) RiderSeat(rider world.Entity) world.RiderSeat {
	pos := happyGhastSeats[0]
	for i, x := range h.state().riders {
		if x == rider.H() {
			pos = happyGhastSeats[i]
		}
	}
	return seat{pos: pos}.riderSeat(rider)
}
func (h *HappyGhast) viewLink(tx *world.Tx, r *world.EntityHandle, on bool) {
	linkType := LinkPassenger
	if h.state().riders[0] == r {
		linkType = LinkRider
	}
	vs := tx.Viewers(h.Position())
	for _, v := range vs {
		if lv, ok := v.(interface {
			ViewEntityLinkType(*world.EntityHandle, *world.EntityHandle, LinkType, bool)
		}); ok {
			lv.ViewEntityLinkType(h.H(), r, linkType, on)
		} else if lv, ok := v.(interface {
			ViewEntityLink(*world.EntityHandle, *world.EntityHandle, bool)
		}); ok {
			lv.ViewEntityLink(h.H(), r, on)
		}
	}
	tx.ReleaseViewers(vs)
}

// Interact ports HappyGhast.mobInteract: a player gets on a harnessed happy
// ghast unless sneaking, and snowballs make a ghastling grow up faster.
func (h *HappyGhast) Interact(tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	if u == nil {
		return false
	}
	b := h.state()
	if b.age < 0 {
		return feedBreeder(h, u, ctx)
	}
	if s, ok := u.(interface{ Sneaking() bool }); b.harness && (!ok || !s.Sneaking()) {
		return h.Mount(tx, u)
	}
	return false
}
func (h *HappyGhast) CanEquipHarness(_ world.Entity) bool {
	return h.state().age >= 0 && !h.state().harness && !h.Dead()
}
func (h *HappyGhast) EquipHarness(tx *world.Tx, _ world.Entity, i world.Item) bool {
	if i == nil || !h.CanEquipHarness(nil) {
		return false
	}
	name, _ := i.EncodeItem()
	if _, ok := world.ItemByName(name, 0); !ok {
		return false
	}
	b := h.state()
	b.harness = true
	b.harnessName = name
	h.viewArmour(tx)
	return true
}
func (h *HappyGhast) ShearHarness(tx *world.Tx, _ world.Entity) bool {
	b := h.state()
	if !b.harness {
		return false
	}
	for _, passenger := range b.riders {
		if passenger != nil {
			if p, ok := passenger.Entity(tx); ok {
				h.Dismount(tx, p)
			}
		}
	}
	if b.harnessName != "" {
		if it, ok := world.ItemByName(b.harnessName, 0); ok {
			tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: h.Position()}, item.NewStack(it, 1)))
		}
	}
	b.harness = false
	b.harnessName = ""
	h.viewArmour(tx)
	return true
}
func (h *HappyGhast) Harnessed() bool        { return h.state().harness }
func (h *HappyGhast) Baby() bool             { return h.state().age < 0 }
func (h *HappyGhast) AgeTicks() int          { return h.state().age }
func (h *HappyGhast) Health() float64        { return h.state().health.Health() }
func (h *HappyGhast) MaxHealth() float64     { return h.state().health.MaxHealth() }
func (h *HappyGhast) SetMaxHealth(v float64) { h.state().health.SetMaxHealth(v) }
func (h *HappyGhast) Dead() bool             { return h.Health() <= 0 }

// Panicking reports whether the ghastling flees from danger.
func (h *HappyGhast) Panicking() bool { return h.state().panicking() }
func (h *HappyGhast) Heal(v float64, _ world.HealingSource) float64 {
	if v <= 0 || h.Dead() {
		return 0
	}
	old := h.Health()
	h.state().health.AddHealth(v)
	return h.Health() - old
}
func (h *HappyGhast) Hurt(v float64, src world.DamageSource) (float64, bool) {
	b := h.state()
	if v <= 0 || h.Dead() {
		return 0, false
	}
	var ok bool
	if v, ok = b.ai.DamageCooldown(h.tx, v); !ok {
		return 0, false
	}
	if v, ok = mobEffectDamage(b.effects, v, src); !ok {
		return 0, false
	}
	old := h.Health()
	b.health.AddHealth(-v)
	b.ai.Hurt(src)
	resetLove(h)
	onMobHurt(h.tx, h)
	if h.Dead() {
		h.viewAction(DeathAction{})
		dropAllDeathLoot(h.tx, h, src)
		_ = h.CloseIn(h.tx)
	} else {
		h.viewAction(HurtAction{})
	}
	return old - h.Health(), true
}
func (h *HappyGhast) KnockBack(src mgl64.Vec3, f, height float64) {
	d := h.Position().Sub(src)
	if d.LenSqr() == 0 {
		return
	}
	h.SetVelocity(h.Velocity().Add(d.Normalize().Mul(f).Add(mgl64.Vec3{0, height, 0})))
}
func (h *HappyGhast) Speed() float64               { return h.state().speed }
func (h *HappyGhast) SetSpeed(v float64)           { h.state().speed = v }
func (h *HappyGhast) AddEffect(e effect.Effect)    { h.state().effects.Add(e, h) }
func (h *HappyGhast) RemoveEffect(t effect.Type)   { h.state().effects.Remove(t, h) }
func (h *HappyGhast) Effects() []effect.Effect     { return h.state().effects.Effects() }
func (h *HappyGhast) hasEffect(t effect.Type) bool { _, ok := h.state().effects.Effect(t); return ok }

// SetAge sets the age of the happy ghast in ticks; negative ages are
// ghastlings growing up.
func (h *HappyGhast) SetAge(age int) { setAge(h, min(age, 0)) }

// InLove always reports false: happy ghasts do not fall in love.
func (h *HappyGhast) InLove() bool { return false }

func (h *HappyGhast) breedingState() *breeding                              { return &h.state().breeding }
func (h *HappyGhast) ageTicks() *int                                        { return &h.state().age }
func (h *HappyGhast) breedingFood(stack item.Stack) bool                    { return inItemTag(stack, "happy_ghast_food") }
func (h *HappyGhast) canFallInLove() bool                                   { return false }
func (h *HappyGhast) breedOffspring(*world.Tx, breeder) *world.EntityHandle { return nil }

func (h *HappyGhast) EquipmentArmour() (item.Stack, item.Stack, item.Stack, item.Stack) {
	return item.Stack{}, item.Stack{}, item.Stack{}, item.Stack{}
}
func (h *HappyGhast) BodyItem() item.Stack {
	b := h.state()
	if !b.harness || b.harnessName == "" {
		return item.Stack{}
	}
	i, ok := world.ItemByName(b.harnessName, 0)
	if !ok {
		return item.Stack{}
	}
	return item.NewStack(i, 1)
}
func (h *HappyGhast) viewAction(a world.EntityAction) {
	vs := h.tx.Viewers(h.Position())
	for _, v := range vs {
		v.ViewEntityAction(h, a)
	}
	h.tx.ReleaseViewers(vs)
}
func (h *HappyGhast) viewArmour(tx *world.Tx) {
	vs := tx.Viewers(h.Position())
	for _, v := range vs {
		v.ViewEntityArmour(h)
	}
	tx.ReleaseViewers(vs)
}

var _ Living = (*HappyGhast)(nil)
var _ VehicleInput = (*HappyGhast)(nil)
var _ VehicleController = (*HappyGhast)(nil)
var _ PassengerDismounter = (*HappyGhast)(nil)
var _ Harnessable = (*HappyGhast)(nil)
var _ Interactable = (*HappyGhast)(nil)
var _ breeder = (*HappyGhast)(nil)
