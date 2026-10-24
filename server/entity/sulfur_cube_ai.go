package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// This file ports the goals and movement of Java's SulfurCube and the
// archetypes of SulfurCubeArchetypes.

// sulfurCubeArchetype ports a SulfurCubeArchetype: the block tag it covers
// and how a cube holding one of its blocks moves, bounces and sounds.
type sulfurCubeArchetype struct {
	name string
	// speed, bounce, friction and drag are the arguments of
	// SulfurCubeArchetypes.archetype: speed lowers the knockback
	// resistance, bounce adds bounciness, and friction and drag scale the
	// friction and air drag modifiers.
	speed, bounce, friction, drag float64
	buoyant                       bool
	explosion                     *sulfurCubeExplosion
	// contactDamage is the damage a hot cube deals what touches it.
	contactDamage          float64
	knockbackH, knockbackV float64
	// pushThreshold is the speed of a push that sounds, and pushCooldown
	// the seconds between such sounds.
	pushThreshold, pushCooldown float64
}

// sulfurCubeExplosion ports SulfurCubeArchetype.ExplosionData.
type sulfurCubeExplosion struct {
	power float64
	fuse  int
}

// sulfurCubeArchetypes are the archetypes SulfurCubeArchetypes registers.
var sulfurCubeArchetypes = []sulfurCubeArchetype{
	{name: "regular", speed: 1, bounce: .5, friction: .3, drag: .1, buoyant: true, knockbackH: .4125, knockbackV: .09, pushThreshold: .2, pushCooldown: .5},
	{name: "bouncy", speed: 2, bounce: .9, friction: .3, drag: .01, buoyant: true, knockbackH: .4125, knockbackV: .105, pushThreshold: .3, pushCooldown: .7},
	{name: "slow_bouncy", speed: -.4, bounce: .6, friction: .3, drag: .05, knockbackH: .4125, knockbackV: .24, pushThreshold: .05, pushCooldown: .5},
	{name: "slow_flat", speed: -.5, bounce: .4, friction: .4, drag: .1, knockbackH: .4125, knockbackV: .105, pushThreshold: .03, pushCooldown: .9},
	{name: "fast_flat", speed: 1, bounce: .5, friction: .2, drag: .01, knockbackH: .9125, knockbackV: .09, pushThreshold: .03, pushCooldown: .9},
	{name: "light", speed: 1, bounce: 1, friction: .3, drag: 1.8, buoyant: true, knockbackH: .4125, knockbackV: .18, pushThreshold: .2, pushCooldown: .7},
	{name: "fast_sliding", speed: -.5, bounce: .1, friction: .05, drag: .01, knockbackH: .6625, knockbackV: .09, pushThreshold: .05, pushCooldown: 1},
	{name: "slow_sliding", speed: -.8, bounce: .1, friction: .05, drag: .01, knockbackH: .4125, knockbackV: .09, pushThreshold: .02, pushCooldown: 1},
	{name: "sticky", speed: 2, bounce: 0, friction: 2, drag: .01, knockbackH: .4125, knockbackV: .09, pushThreshold: .05, pushCooldown: .5},
	{name: "high_resistance", speed: -.7, bounce: .2, friction: 1, drag: .01, knockbackH: .4125, knockbackV: .09, pushThreshold: .03, pushCooldown: .7},
	{name: "explosive", speed: 1, bounce: .5, friction: .3, drag: .3, buoyant: true, explosion: &sulfurCubeExplosion{power: 3, fuse: 120}, knockbackH: .4125, knockbackV: .09, pushThreshold: .1, pushCooldown: .7},
	{name: "hot", speed: 1, bounce: .5, friction: .3, drag: .1, buoyant: true, contactDamage: 1, knockbackH: .4125, knockbackV: .09, pushThreshold: .2, pushCooldown: .7},
}

// sulfurCubeArchetypeOf returns the archetype of the block passed, nil if it
// has none.
func sulfurCubeArchetypeOf(stack item.Stack) *sulfurCubeArchetype {
	if stack.Empty() {
		return nil
	}
	for i := range sulfurCubeArchetypes {
		if a := &sulfurCubeArchetypes[i]; inItemTag(stack, "sulfur_cube_archetype/"+a.name) {
			return a
		}
	}
	return nil
}

// knockbackResistance is the knockback resistance attribute of a cube of
// the archetype, the base of 0 less its speed, which ranges from 0 to 1.
func (a *sulfurCubeArchetype) knockbackResistance() float64 {
	return math.Max(0, math.Min(1, -a.speed))
}

func (a *sulfurCubeArchetype) hitSound() string  { return "entity.sulfur_cube." + a.name + ".hit" }
func (a *sulfurCubeArchetype) pushSound() string { return "entity.sulfur_cube." + a.name + ".push" }

func (b *SulfurCubeBehaviour) mobAI() *MobAI   { return &b.ai }
func (b *SulfurCubeBehaviour) cube() *cubeAI   { return &b.cubeAI }
func (b *SulfurCubeBehaviour) cubeSize() int   { return b.size }
func (b *SulfurCubeBehaviour) cubeMagma() bool { return false }

// cubeDealsDamage ports SulfurCube.canDealDamage: sulfur cubes never attack.
func (b *SulfurCubeBehaviour) cubeDealsDamage() bool { return false }

// cubeSoundName returns the sound of a sulfur cube for the cube sound named,
// as SulfurCube.getJumpSound and getSquishSound pick them.
func (b *SulfurCubeBehaviour) cubeSoundName(name string) string {
	if b.size <= 1 {
		return "entity.small_sulfur_cube." + name
	}
	if name == "squish" && !b.bodyItem.Empty() {
		return "entity.sulfur_cube.bounce"
	}
	return "entity.sulfur_cube." + name
}

// Tick runs a tick of a sulfur cube: SulfurCube.tickFuse and
// primeWhenOnPoweredPosition, its growing up and its MobAI.
func (b *SulfurCubeBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	s := &SulfurCube{Ent: e}
	if b.fuse > 0 {
		b.fuse--
	}
	if b.archetype != nil && b.archetype.explosion != nil && b.fuse == 0 {
		src := world.NewEntityExplosionSource(s, b.archetype.explosion.power)
		if gameRuleTNTExplodes(tx) {
			cfg := block.ExplosionConfig{NoBlockDamage: mobGriefingDisabled(tx)}
			cfg.Explode(tx, src)
		}
		b.fuse = -1
		s.die(ExplosionDamageSource{Source: src})
		return nil
	}
	if s.canExplode() && tx.RedstonePower(cube.PosFromVec3(e.data.Pos)) != 0 {
		s.primeTime(false)
	}
	// AgeableMob.aiStep: a baby grows into a cube of size two.
	if b.age < 0 {
		if b.age++; b.age == 0 {
			b.size = 2
			b.health.SetMaxHealth(8)
			b.health.AddHealth(8 - b.health.Health())
			e.updateState()
		}
	} else if b.age > 0 {
		b.age--
	}
	ai := &b.ai
	ai.EyeHeight = .175 * float64(b.size)
	held := !b.bodyItem.Empty()
	// A cube holding a block stands still: it neither hops nor steps up, and
	// moves alike in all directions through the air.
	ai.MovementSpeed = .2 + .1*float64(b.size)
	if held {
		ai.MovementSpeed = 0
	}
	if ai.started {
		ai.StepHeight = .6
		if held {
			ai.StepHeight = -1
		}
	}
	ai.Bounciness, ai.FrictionModifier, ai.AirDragModifier = 0, 1, 1
	if a := b.archetype; a != nil {
		ai.Bounciness = math.Min(1, a.bounce)
		ai.FrictionModifier, ai.AirDragModifier = a.friction, a.drag
	}
	m := ai.Mob(e, s)
	if ai.started && held == b.goalsRegistered {
		// SulfurCube.collectEquipmentChanges drops every goal when the cube
		// swallows a block and registers them again when it lets go.
		if held {
			ai.Goals.Remove(m, func(Goal) bool { return true })
			m.SetSpeed(0)
			b.goalsRegistered = false
		} else {
			b.registerGoals(m)
		}
	}
	mov := ai.Tick(e, s, func(m *Mob) {
		b.move.yRot = 180 * m.Yaw() / math.Pi
		ai.moveOverride, ai.lookOverride = sulfurCubeMoveControl{b}, sulfurCubeLookControl{b}
		ai.nav.SetCanFloat(true)
		if !held {
			b.registerGoals(m)
		}
	})
	if !b.dead {
		tickCube(m, b)
		b.touch(m)
	}
	return mov
}

// registerGoals ports AbstractCubeMob.registerGoals with the goals
// SulfurCube adds: it follows players holding what it likes and turns to
// items nearby it can swallow.
func (b *SulfurCubeBehaviour) registerGoals(m *Mob) {
	m.AI.Goals.Add(1, &cubeFloatGoal{goalFlags{GoalJump | GoalMove}})
	m.AI.Goals.Add(4, &cubeRandomDirectionGoal{goalFlags: goalFlags{GoalLook}})
	m.AI.Goals.Add(5, &cubeKeepOnJumpingGoal{goalFlags{GoalJump | GoalMove}})
	b.tempt = NewTemptGoal(1, func(st item.Stack) bool {
		if b.age < 0 {
			return sulfurCubeFood(st)
		}
		return sulfurCubeSwallowable(st)
	}, false)
	b.tempt.goalFlags = goalFlags{GoalLook}
	b.tempt.StopDistance, b.tempt.Range = 1, 8
	b.tempt.StopNavigation = func(*Mob) { b.move.setWantedMovement(0) }
	b.tempt.NavigateTowards = func(m *Mob, p world.Entity) {
		m.LookAt(p, 10, 10)
		b.move.setDirection(m.Yaw(), true)
	}
	m.AI.Goals.Add(2, b.tempt)
	m.AI.Goals.Add(3, &sulfurCubeSearchForItemsGoal{goalFlags: goalFlags{GoalLook}, cube: b})
	b.goalsRegistered = true
}

// customServerAiStep ports SulfurCube.customServerAiStep and Mob.aiStep
// picking up loot: an adult cube without a block swallows one it touches.
func (b *SulfurCubeBehaviour) customServerAiStep(m *Mob) {
	if b.pickupTimer > 0 {
		b.pickupTimer--
	}
	if b.pushSoundCooldown > 0 {
		b.pushSoundCooldown--
	}
	if !b.bodyItem.Empty() || b.age < 0 || b.pickupTimer > 0 || mobGriefingDisabled(m.tx) {
		return
	}
	// Mob.getPickupReach reaches a block further sideways, and the items
	// whose boxes touch that reach are picked up.
	box := m.BBox().GrowVec3(mgl64.Vec3{1, 0, 1})
	for e := range m.tx.EntitiesWithin(box.Grow(1)) {
		stack, ok := pickableItem(e)
		if !ok || !sulfurCubeSwallowable(stack) || !e.H().Type().BBox(e).Translate(e.Position()).IntersectsWith(box) {
			continue
		}
		s := m.Self.(*SulfurCube)
		s.setBodyItem(item.NewStack(stack.Item(), 1))
		m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "entity.sulfur_cube.absorb", Volume: 1, Pitch: 1})
		takeItemEntity(m, e, 1)
		return
	}
}

// omnidirectionalAirMover ports SulfurCube.omnidirectionalAirMover: a cube
// holding a block slows down as much up and down as sideways.
func (b *SulfurCubeBehaviour) omnidirectionalAirMover() bool { return !b.bodyItem.Empty() }

// jumpFromGround ports AbstractCubeMob.jumpFromGround: the cube jumps at
// its jump power whatever it moved up at.
func (b *SulfurCubeBehaviour) jumpFromGround(m *Mob) bool {
	m.data.Vel[1] = m.AI.jumpPower(m)
	return true
}

func (b *SulfurCubeBehaviour) jumpInLiquid(*Mob, bool) bool { return false }

// travel ports SulfurCube.travelInFluid: a cube holding a buoyant block
// bobs up to the surface of water and lava.
func (b *SulfurCubeBehaviour) travel(m *Mob, input mgl64.Vec3) bool {
	if !m.AI.travelsInFluid(m) || b.bodyItem.Empty() || b.archetype == nil || !b.archetype.buoyant {
		return false
	}
	m.AI.travelInFluid(m, input)
	height := m.AI.waterHeight
	if !m.InWater() {
		height = m.AI.lavaHeight
	}
	vibe := .2 * math.Sin(float64(m.AI.tickCount)*.4)
	if immersion := height - m.BBox().Height()*.2 + vibe; immersion > 0 {
		m.data.Vel[1] += math.Min(1, immersion) * float64(float32(.04))
	}
	return true
}

// touch ports SulfurCube.playerTouch and doPush: players walking into a cube
// holding a block push it along, faster the faster they go, and a hot cube
// burns what it touches.
func (b *SulfurCubeBehaviour) touch(m *Mob) {
	box := m.BBox()
	held := !b.bodyItem.Empty()
	for p := range m.tx.Players() {
		if spectator(p) || !entityAlive(p) || !held {
			continue
		}
		pbox := p.H().Type().BBox(p).Translate(p.Position())
		if !pbox.GrowVec3(mgl64.Vec3{1, .5, 1}).IntersectsWith(box) {
			continue
		}
		b.playerPush(m, p)
	}
	if b.archetype == nil || b.archetype.contactDamage == 0 {
		return
	}
	for e := range m.tx.EntitiesWithin(box.Grow(.1)) {
		if e.H() == m.H() || !pushable(e) || !e.H().Type().BBox(e).Translate(e.Position()).IntersectsWith(box) {
			continue
		}
		if l, ok := e.(Living); ok {
			src := sulfurCubeHotDamageSource{}
			if isPlayerEntity(e) {
				src.Cube = m.Self
			}
			l.Hurt(b.archetype.contactDamage, src)
		}
	}
}

// playerPush ports SulfurCube.playerPush: a player beside the cube pushes it
// away from them at up to half a block a tick.
func (b *SulfurCubeBehaviour) playerPush(m *Mob, p world.Entity) {
	toCube := m.data.Pos.Sub(p.Position())
	feet, bottom := p.Position()[1], m.data.Pos[1]
	top := bottom + m.BBox().Height()
	if math.Hypot(toCube[0], toCube[2]) >= float64(float32(1.3)) || feet > top || feet+p.H().Type().BBox(p).Height() <= bottom {
		return
	}
	resistance := 0.0
	if b.archetype != nil {
		resistance = b.archetype.knockbackResistance()
	}
	knockback := math.Max(0, 1-resistance)
	dir := mgl64.Vec3{toCube[0], 0, toCube[2]}
	if dir.Len() > 0 {
		dir = dir.Normalize().Mul(knockback)
	}
	scale := float64(float32(.3))
	if isPassenger(p) {
		scale = float64(float32(.16))
	}
	speed := 0.0
	if v, ok := p.(interface{ Velocity() mgl64.Vec3 }); ok {
		speed = v.Velocity().Len()
	}
	speed = math.Max(0, math.Min(.5, speed*2*scale))
	up := 0.0
	if m.OnGround() {
		up = knockback * float64(float32(.3))
	}
	push := mgl64.Vec3{dir[0], up, dir[2]}.Mul(speed)
	if a := b.archetype; a != nil && push.LenSqr() > a.pushThreshold*a.pushThreshold && b.pushSoundCooldown <= 0 {
		b.pushSoundCooldown = int(a.pushCooldown * 20)
		m.tx.PlaySound(m.data.Pos, sound.Custom{Name: a.pushSound(), Volume: 1, Pitch: 1})
	}
	m.data.Vel = m.data.Vel.Add(push)
	if b.archetype != nil && b.archetype.contactDamage > 0 {
		if l, ok := p.(Living); ok {
			l.Hurt(b.archetype.contactDamage, sulfurCubeHotDamageSource{Cube: m.Self})
		}
	}
}

// sulfurCubeLookControl ports SulfurCube.SulfurCubeLookControl: a cube
// holding a block turns square to the nearest side instead of looking
// about.
type sulfurCubeLookControl struct{ cube *SulfurCubeBehaviour }

func (c sulfurCubeLookControl) tick(m *Mob) {
	if c.cube.bodyItem.Empty() {
		m.AI.look.tick(m)
		return
	}
	yaw := m.Yaw()
	yaw -= wrapDegrees90(yaw)
	m.SetYaw(yaw)
	m.AI.headYaw = yaw
}

// sulfurCubeMoveControl ports SulfurCube.SulfurCubeMobMoveControl: a cube
// holding a block does not hop.
type sulfurCubeMoveControl struct{ cube *SulfurCubeBehaviour }

func (c sulfurCubeMoveControl) tick(m *Mob) {
	if c.cube.bodyItem.Empty() {
		c.cube.move.tick(m)
	}
}

// wrapDegrees90 ports Mth.wrapDegrees90: the angle within 45 degrees of a
// right angle it is closest to, relative to it.
func wrapDegrees90(a float64) float64 {
	a = math.Mod(a, 90)
	if a >= 45 {
		a -= 90
	}
	if a < -45 {
		a += 90
	}
	return a
}

// sulfurCubeSearchForItemsGoal ports SulfurCube.SulfurCubeSearchForItemsGoal:
// an adult cube turns to the nearest item within eight blocks it can
// swallow and hops at it.
type sulfurCubeSearchForItemsGoal struct {
	goalFlags
	cube   *SulfurCubeBehaviour
	target *world.EntityHandle
}

func (g *sulfurCubeSearchForItemsGoal) CanUse(m *Mob) bool {
	if g.cube.age < 0 || g.cube.pickupTimer > 0 {
		return false
	}
	var nearest world.Entity
	best := math.MaxFloat64
	box := m.BBox().Grow(8)
	for e := range m.tx.EntitiesWithin(box.Grow(1)) {
		stack, ok := pickableItem(e)
		if !ok || !sulfurCubeSwallowable(stack) || !e.H().Type().BBox(e).Translate(e.Position()).IntersectsWith(box) {
			continue
		}
		if d := m.DistanceToSqr(e); d < best {
			nearest, best = e, d
		}
	}
	if nearest == nil {
		return false
	}
	g.target = nearest.H()
	return true
}

func (g *sulfurCubeSearchForItemsGoal) CanContinueToUse(m *Mob) bool { return g.CanUse(m) }

func (g *sulfurCubeSearchForItemsGoal) Tick(m *Mob) {
	if e, ok := g.target.Entity(m.tx); ok {
		m.LookAt(e, 10, 10)
	}
	g.cube.move.setDirection(m.Yaw(), true)
}

// pickableItem returns the stack of an item entity a mob may pick up: one
// without a pickup delay.
func pickableItem(e world.Entity) (item.Stack, bool) {
	ent, ok := e.(*Ent)
	if !ok {
		return item.Stack{}, false
	}
	i, ok := ent.Behaviour().(*ItemBehaviour)
	if !ok || i.i.Empty() || i.pickupDelay > 0 {
		return item.Stack{}, false
	}
	return i.i, true
}

// takeItemEntity takes count items off an item entity, removing it once
// empty, as Mob.take.
func takeItemEntity(m *Mob, e world.Entity, count int) {
	ent := e.(*Ent)
	i := ent.Behaviour().(*ItemBehaviour)
	i.i = i.i.Grow(-count)
	if i.i.Empty() {
		_ = ent.CloseIn(m.tx)
	}
}

// sulfurCubeHotDamageSource is the sulfur_cube_hot damage a hot cube deals
// what touches it. Cube is set when the damage is attributed to the cube,
// as it is for players.
type sulfurCubeHotDamageSource struct{ Cube world.Entity }

func (sulfurCubeHotDamageSource) ReducedByResistance() bool { return true }
func (sulfurCubeHotDamageSource) ReducedByArmour() bool     { return false }
func (sulfurCubeHotDamageSource) Fire() bool                { return true }
func (sulfurCubeHotDamageSource) IgnoreTotem() bool         { return false }
