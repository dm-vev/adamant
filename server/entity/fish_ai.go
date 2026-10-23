package entity

import (
	"math"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// This file ports the goals of Java's AbstractFish, AbstractSchoolingFish and
// Pufferfish, which cod, salmon, tropical fish, pufferfish and tadpoles swim
// with.

// mobAI returns the MobAI of a fish, or nil for an axolotl, which does not
// run on one.
func (b *AquaticMobBehaviour) mobAI() *MobAI {
	if b.id == "minecraft:axolotl" {
		return nil
	}
	return &b.ai
}

// schooling reports if the fish extends Java's AbstractSchoolingFish.
func (b *AquaticMobBehaviour) schooling() bool {
	switch b.id {
	case "minecraft:cod", "minecraft:salmon", "minecraft:tropical_fish":
		return true
	}
	return false
}

// maxSchoolSize ports AbstractSchoolingFish.getMaxSchoolSize.
func (b *AquaticMobBehaviour) maxSchoolSize() int {
	if b.id == "minecraft:salmon" {
		return 5
	}
	return 8
}

// tickFish runs a tick of a fish: its air out of water, the puffing of a
// pufferfish, its flopping on land and its MobAI.
func (b *AquaticMobBehaviour) tickFish(e *Ent, tx *world.Tx) *Movement {
	self := aquaticWorldEntity(e)
	ai := &b.ai
	ai.MovementSpeed = b.speed
	ai.EyeHeight = e.H().Type().BBox(self).Height() * .65
	// WaterAnimal.handleAirSupply: a fish out of water drowns after 15
	// seconds.
	if _, inWater := tx.Liquid(cube.PosFromVec3(e.data.Pos)); inWater {
		b.airTicks = 0
	} else if b.airTicks++; b.airTicks > 300 && b.airTicks%20 == 0 {
		self.(Living).Hurt(2, DrowningDamageSource{})
		if b.dead {
			return nil
		}
	}
	m := ai.Mob(e, self)
	if ai.started {
		if b.id == "minecraft:pufferfish" {
			b.tickPuffState(m)
		}
		if b.schooling() && b.schoolSize > 1 && m.Random().NextInt(200) == 1 {
			b.checkSchool(m)
		}
		// AbstractFish.aiStep: a fish on land flops about.
		if !m.InWater() && ai.onGround && ai.collidedVertically {
			r := m.Random()
			m.data.Vel = m.data.Vel.Add(mgl64.Vec3{float64((r.NextFloat()*2 - 1) * .05), float64(float32(.4)), float64((r.NextFloat()*2 - 1) * .05)})
			ai.onGround = false
			tx.PlaySound(e.data.Pos, sound.Custom{Name: aquaticFlopSound(b.id), Volume: 1, Pitch: 1})
		}
	}
	mv := ai.Tick(e, self, b.registerFishGoals)
	if b.id == "minecraft:tadpole" {
		if !b.breeding.ageLocked {
			b.age++
		}
		if b.age >= 24000 {
			opts := world.EntitySpawnOpts{Position: e.Position(), Rotation: e.data.Rot, Velocity: e.data.Vel}
			tx.AddEntity(opts.New(FrogType, frogConfig{variant: frogVariantAt(tx, e.Position())}))
			_ = e.CloseIn(tx)
		}
	}
	return mv
}

// registerFishGoals ports AbstractFish.registerGoals and those of its
// subclasses, with the navigation and move control of AbstractFish.
func (b *AquaticMobBehaviour) registerFishGoals(m *Mob) {
	ai := m.AI
	ai.nav.SetWaterBound(false)
	// WaterAnimal: water costs nothing.
	ai.SetPathfindingMalus(PathWater, 0)
	ai.moveOverride = fishMoveControl{}
	b.panic = NewPanicGoal(1.25)
	ai.Goals.Add(0, b.panic)
	if b.id == "minecraft:pufferfish" {
		ai.Goals.Add(1, &pufferfishPuffGoal{fish: b})
	}
	ai.Goals.Add(2, NewAvoidEntityGoal(func(e world.Entity) bool { return isPlayerEntity(e) && !spectator(e) }, 8, 1.6, 1.4))
	if b.id == "minecraft:tadpole" {
		// The brain of Tadpole follows players holding slime balls.
		ai.Goals.Add(3, NewTemptGoal(1.25, func(s item.Stack) bool { _, ok := s.Item().(item.Slimeball); return ok }, false))
	}
	swim := NewRandomStrollGoal(1)
	swim.Interval, swim.Position = 40, randomSwimmablePos
	ai.Goals.Add(4, fishSwimGoal{swim, b})
	if b.schooling() {
		ai.Goals.Add(5, newFollowFlockLeaderGoal(m, b))
	}
}

// travel ports AbstractFish.travelInWater: a fish swims slowly and sinks
// unless it chases something.
func (b *AquaticMobBehaviour) travel(m *Mob, input mgl64.Vec3) bool {
	if !m.InWater() {
		return false
	}
	m.AI.moveRelative(m, float64(float32(.01)), input)
	m.AI.moveSelf(m, m.data.Vel)
	m.data.Vel = m.data.Vel.Mul(.9)
	if m.Target() == nil {
		m.data.Vel[1] -= .005
	}
	return true
}

// randomSwimmablePos ports BehaviorUtils.getRandomSwimmablePos: a random
// position within ten blocks sideways and seven up or down in water the mob
// swims through, after at most ten more tries.
func randomSwimmablePos(m *Mob) (mgl64.Vec3, bool) {
	pos, ok := DefaultRandomPos(m, 10, 7)
	for count := 0; ok && !pathfindableWater(m, cube.PosFromVec3(pos)) && count < 10; count++ {
		pos, ok = DefaultRandomPos(m, 10, 7)
	}
	return pos, ok
}

// eyeInWater reports if the eyes of a mob are under water, as
// Entity.isEyeInFluid for water.
func eyeInWater(m *Mob) bool {
	eye := mgl64.Vec3{m.data.Pos[0], m.EyeY(), m.data.Pos[2]}
	pos := cube.PosFromVec3(eye)
	l, ok := m.tx.Liquid(pos)
	return ok && l.LiquidType() == "water" && eye[1] < float64(pos.Y())+liquidHeight(m.tx, pos, l)
}

// fishMoveControl ports AbstractFish.FishMoveControl: a fish under water
// floats up a little, and swims towards where it wants to go while its
// navigation has a path.
type fishMoveControl struct{}

func (fishMoveControl) tick(m *Mob) {
	if eyeInWater(m) {
		m.data.Vel[1] += .005
	}
	mc := &m.AI.move
	if mc.operation != moveTo || m.AI.nav.IsDone() {
		m.SetSpeed(0)
		return
	}
	target := mc.speedModifier * m.AI.MovementSpeed
	m.SetSpeed(m.AI.speed + (target-m.AI.speed)*float64(float32(.125)))
	xd, yd, zd := mc.wanted[0]-m.data.Pos[0], mc.wanted[1]-m.data.Pos[1], mc.wanted[2]-m.data.Pos[2]
	if yd != 0 {
		dd := math.Sqrt(xd*xd + yd*yd + zd*zd)
		m.data.Vel[1] += m.AI.speed * (yd / dd) * .1
	}
	if xd != 0 || zd != 0 {
		yaw := moveRotlerp(m.Yaw(), math.Atan2(zd, xd)*180/math.Pi-90, 90)
		m.SetYaw(yaw)
		m.AI.bodyYaw = yaw
	}
}

// fishSwimGoal ports AbstractFish.FishSwimGoal, which a fish following a
// school does not run.
type fishSwimGoal struct {
	*RandomStrollGoal
	fish *AquaticMobBehaviour
}

func (g fishSwimGoal) CanUse(m *Mob) bool {
	return !g.fish.isFollower(m.tx) && g.RandomStrollGoal.CanUse(m)
}

// isFollower ports AbstractSchoolingFish.isFollower: the fish follows a
// living leader.
func (b *AquaticMobBehaviour) isFollower(tx *world.Tx) bool {
	if b.leader == nil || b.leaderState == nil || b.leaderState.dead {
		return false
	}
	_, ok := b.leader.Entity(tx)
	return ok
}

// startFollowing ports AbstractSchoolingFish.startFollowing.
func (b *AquaticMobBehaviour) startFollowing(leader *world.EntityHandle, state *AquaticMobBehaviour) {
	b.leader, b.leaderState = leader, state
	state.schoolSize++
}

// stopFollowing ports AbstractSchoolingFish.stopFollowing.
func (b *AquaticMobBehaviour) stopFollowing() {
	if b.leaderState != nil {
		b.leaderState.schoolSize--
	}
	b.leader, b.leaderState = nil, nil
}

// canBeFollowed ports AbstractSchoolingFish.canBeFollowed.
func (b *AquaticMobBehaviour) canBeFollowed() bool {
	return b.schoolSize > 1 && b.schoolSize < b.maxSchoolSize()
}

// checkSchool ports the part of AbstractSchoolingFish.tick that breaks up a
// school with no fish of its kind within eight blocks.
func (b *AquaticMobBehaviour) checkSchool(m *Mob) {
	neighbours := 0
	for e := range m.tx.EntitiesWithin(m.BBox().Grow(8)) {
		if e.H().Type() == m.H().Type() {
			neighbours++
		}
	}
	if neighbours <= 1 {
		b.schoolSize = 1
	}
}

// followFlockLeaderGoal ports FollowFlockLeaderGoal: every ten seconds or so
// a fish without a school forms one with the fish of its kind within eight
// blocks, and a follower swims after its leader while within eleven blocks
// of it.
type followFlockLeaderGoal struct {
	goalFlags
	fish                            *AquaticMobBehaviour
	timeToRecalcPath, nextStartTick int
}

func newFollowFlockLeaderGoal(m *Mob, fish *AquaticMobBehaviour) *followFlockLeaderGoal {
	g := &followFlockLeaderGoal{fish: fish}
	g.nextStartTick = g.startDelay(m)
	return g
}

func (g *followFlockLeaderGoal) startDelay(m *Mob) int {
	return reducedTickDelay(200 + int(m.Random().NextInt(200))%20)
}

func (g *followFlockLeaderGoal) CanUse(m *Mob) bool {
	b := g.fish
	if b.schoolSize > 1 {
		return false
	}
	if b.isFollower(m.tx) {
		return true
	}
	if g.nextStartTick > 0 {
		g.nextStartTick--
		return false
	}
	g.nextStartTick = g.startDelay(m)
	var candidates []*AquaticMob
	var leader *AquaticMob
	for e := range m.tx.EntitiesWithin(m.BBox().Grow(8)) {
		if e.H().Type() != m.H().Type() {
			continue
		}
		other := aquaticOf(e)
		if other == nil {
			continue
		}
		if ob := other.behaviour(); ob.canBeFollowed() || !ob.isFollower(m.tx) {
			candidates = append(candidates, other)
			if leader == nil && ob.canBeFollowed() {
				leader = other
			}
		}
	}
	if leader == nil {
		leader = &AquaticMob{Ent: m.Ent}
	}
	// AbstractSchoolingFish.addFollowers takes as many of the fish that
	// follow no leader as fit in the school, the leader among them.
	lb := leader.behaviour()
	room := lb.maxSchoolSize() - lb.schoolSize
	for _, other := range candidates {
		ob := other.behaviour()
		if ob.isFollower(m.tx) {
			continue
		}
		if room <= 0 {
			break
		}
		room--
		if other.H() != leader.H() {
			ob.startFollowing(leader.H(), lb)
		}
	}
	return b.isFollower(m.tx)
}

func (g *followFlockLeaderGoal) CanContinueToUse(m *Mob) bool {
	b := g.fish
	if !b.isFollower(m.tx) {
		return false
	}
	leader, _ := b.leader.Entity(m.tx)
	return m.DistanceToSqr(leader) <= 121
}

func (g *followFlockLeaderGoal) Start(*Mob) { g.timeToRecalcPath = 0 }

func (g *followFlockLeaderGoal) Stop(*Mob) { g.fish.stopFollowing() }

func (g *followFlockLeaderGoal) Tick(m *Mob) {
	if g.timeToRecalcPath--; g.timeToRecalcPath > 0 {
		return
	}
	g.timeToRecalcPath = adjustedTickDelay(g, 10)
	// AbstractSchoolingFish.pathToLeader.
	if g.fish.isFollower(m.tx) {
		leader, _ := g.fish.leader.Entity(m.tx)
		m.AI.nav.MoveToEntity(m, leader, 1)
	}
}

// aquaticOf returns the aquatic mob of an entity, or nil.
func aquaticOf(e world.Entity) *AquaticMob {
	switch a := e.(type) {
	case *Cod:
		return a.AquaticMob
	case *Salmon:
		return a.AquaticMob
	case *TropicalFish:
		return a.AquaticMob
	case *Pufferfish:
		return a.AquaticMob
	case *Tadpole:
		return a.AquaticMob
	case *Axolotl:
		return a.AquaticMob
	}
	return nil
}

// scaresPufferfish ports Pufferfish.SCARY_MOB and the targeting conditions
// pufferfish test with: a living entity other than the fish itself, a
// spectator or a creative player, and not in #not_scary_for_pufferfish.
func scaresPufferfish(self, target world.Entity) bool {
	if target.H() == self.H() || !entityAlive(target) || spectator(target) {
		return false
	}
	if isPlayerEntity(target) {
		if mode, ok := target.(interface{ GameMode() world.GameMode }); ok && mode.GameMode().CreativeInventory() {
			return false
		}
	}
	notScary, _ := gamedata.EntityTypeTagContains("not_scary_for_pufferfish", target.H().Type().EncodeEntity())
	return !notScary
}

// pufferfishPuffGoal ports PufferfishPuffGoal: a pufferfish puffs up while
// something that scares it is within two blocks.
type pufferfishPuffGoal struct {
	goalFlags
	fish *AquaticMobBehaviour
}

func (g *pufferfishPuffGoal) CanUse(m *Mob) bool {
	for e := range m.tx.EntitiesWithin(m.BBox().Grow(2)) {
		if _, living := e.(Living); living && scaresPufferfish(m.Self, e) {
			return true
		}
	}
	return false
}

func (g *pufferfishPuffGoal) Start(*Mob) { g.fish.inflateCounter, g.fish.deflateTimer = 1, 0 }

func (g *pufferfishPuffGoal) Stop(*Mob) { g.fish.inflateCounter = 0 }

// tickPuffState ports what Pufferfish.tick does before the AI: a pufferfish
// puffs up halfway at once and fully after two seconds, and deflates three
// and five seconds after it calmed down.
func (b *AquaticMobBehaviour) tickPuffState(m *Mob) {
	state := b.puffState
	switch {
	case b.inflateCounter > 0:
		if b.puffState == 0 {
			b.puffState = 1
		} else if b.inflateCounter > 40 && b.puffState == 1 {
			b.puffState = 2
		}
		b.inflateCounter++
	case b.puffState != 0:
		if b.deflateTimer > 60 && b.puffState == 2 {
			b.puffState = 1
		} else if b.deflateTimer > 100 && b.puffState == 1 {
			b.puffState = 0
		}
		b.deflateTimer++
	}
	if b.puffState == state {
		return
	}
	name := "entity.puffer_fish.blow_out"
	if b.puffState > state {
		name = "entity.puffer_fish.blow_up"
	}
	m.tx.PlaySound(m.data.Pos, sound.Custom{Name: name, Volume: 1, Pitch: 1})
	b.viewPuffState(m.Ent)
}

// afterAiStep ports what Pufferfish.aiStep does after the AI: a puffed up
// pufferfish stings the mobs it touches, and Pufferfish.playerTouch the
// players that touch it.
func (b *AquaticMobBehaviour) afterAiStep(m *Mob) {
	if b.id != "minecraft:pufferfish" || b.puffState == 0 || b.dead {
		return
	}
	box := m.BBox()
	for e := range m.tx.EntitiesWithin(box.GrowVec3(mgl64.Vec3{1, .5, 1})) {
		if !scaresPufferfish(m.Self, e) {
			continue
		}
		l, ok := e.(Living)
		if !ok {
			continue
		}
		touches := e.H().Type().BBox(e).Translate(e.Position()).IntersectsWith(box.Grow(.3))
		if isPlayerEntity(e) {
			// Player.touch reaches a block out sideways and half a block up
			// and down.
			touches = e.H().Type().BBox(e).Translate(e.Position()).GrowVec3(mgl64.Vec3{1, .5, 1}).IntersectsWith(box)
		}
		if !touches || !isMobEntity(e) && !isPlayerEntity(e) {
			continue
		}
		if _, hurt := l.Hurt(float64(1+b.puffState), AttackDamageSource{Attacker: m.Self}); hurt {
			l.AddEffect(effect.New(effect.Poison, 1, time.Duration(60*b.puffState)*time.Second/20))
			if !isPlayerEntity(e) {
				m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "entity.puffer_fish.sting", Volume: 1, Pitch: 1})
			}
		}
	}
}
