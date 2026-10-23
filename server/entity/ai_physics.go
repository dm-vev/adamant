package entity

import (
	"math"
	"slices"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// mobAIBehaviour is implemented by the behaviours of mobs driven by a MobAI.
type mobAIBehaviour interface {
	mobAI() *MobAI
}

// mobAI returns the MobAI of the entity and the entity itself, if its
// behaviour has one.
func (e *Ent) mobAI() (*MobAI, *Ent) {
	if b, ok := e.data.Data.(mobAIBehaviour); ok {
		return b.mobAI(), e
	}
	return nil, e
}

// HeadYaw returns the yaw of the head of the entity, which is that of its
// body unless a MobAI turns it.
func (e *Ent) HeadYaw() float64 {
	if ai, _ := e.mobAI(); ai != nil && ai.started {
		return ai.headYaw
	}
	return e.data.Rot.Yaw()
}

// mobImmobile reports if a mob may not act, as LivingEntity.isImmobile: a
// stunned or roaring ravager for one.
func mobImmobile(m *Mob) bool {
	i, ok := m.Behaviour().(interface{ immobile() bool })
	return ok && i.immobile()
}

// mobMoveControl is a move control that replaces the MoveControl of a mob,
// such as the CubeMobMoveControl of slimes.
type mobMoveControl interface {
	tick(m *Mob)
}

// mobJumper is implemented by behaviours of mobs that override
// LivingEntity.jumpFromGround or Mob.jumpInLiquid. Each reports false to
// jump as other mobs do.
type mobJumper interface {
	jumpFromGround(m *Mob) bool
	jumpInLiquid(m *Mob, lava bool) bool
}

// mobTraveller is implemented by behaviours of mobs that override
// LivingEntity.travel. It reports false to travel as other mobs do.
type mobTraveller interface {
	travel(m *Mob, input mgl64.Vec3) bool
}

// mobJumpControl is implemented by behaviours of mobs with a JumpControl of
// their own, which runs in place of the default one.
type mobJumpControl interface {
	tickJumpControl(m *Mob)
}

// mobCustomStep is implemented by behaviours with an AI step of their own
// after their goals, as Mob.customServerAiStep.
type mobCustomStep interface {
	customServerAiStep(m *Mob)
}

// mobWaterSlowDown is implemented by behaviours of mobs that swim at another
// speed, as LivingEntity.getWaterSlowDown.
type mobWaterSlowDown interface {
	waterSlowDown() float64
}

// Tick runs a tick of the mob: its AI and the movement of a LivingEntity,
// ported from LivingEntity.aiStep and Mob.serverAiStep. register adds the
// goals of the mob the first time it ticks. The movement of the tick is
// returned.
func (ai *MobAI) Tick(e *Ent, self world.Entity, register func(m *Mob)) *Movement {
	m := ai.Mob(e, self)
	first := !ai.started
	ai.start(m)
	if first && register != nil {
		register(m)
	}
	prevPos, prevVel, prevRot, prevHead := e.data.Pos, e.data.Vel, e.data.Rot, ai.headYaw
	ai.prevPos = prevPos
	ai.tickCount++
	ai.updateFluids(m)
	if ai.lastHurtBy != nil && (ai.tickCount-ai.lastHurtByTime > 100 || !m.Alive()) {
		ai.lastHurtBy = nil
	}
	ai.resetNoActionTimeNearPlayers(m)
	if e.data.Riding != nil {
		if !ai.NoAI && m.Alive() {
			ai.serverAiStep(m)
		}
		return ai.riderMovement(m, prevPos, prevRot, prevHead)
	}
	ai.aiStep(m)
	if ai.bodyOverride != nil {
		ai.bodyOverride.tick(m)
	} else {
		ai.body.tick(m)
	}
	return newMobMovement(m, prevPos, prevVel, prevRot, prevHead)
}

// resetNoActionTimeNearPlayers resets the no action time of the mob while a
// player is within 32 blocks, as Mob.checkDespawn does.
func (ai *MobAI) resetNoActionTimeNearPlayers(m *Mob) {
	for p := range m.tx.Players() {
		if spectator(p) {
			continue
		}
		if p.Position().Sub(m.data.Pos).LenSqr() < 32*32 {
			ai.noActionTime = 0
			return
		}
	}
}

// aiStep ports LivingEntity.aiStep for a mob.
func (ai *MobAI) aiStep(m *Mob) {
	if ai.noJumpDelay > 0 {
		ai.noJumpDelay--
	}
	v := m.data.Vel
	for _, i := range [3]int{0, 1, 2} {
		if math.Abs(v[i]) < .003 {
			v[i] = 0
		}
	}
	m.data.Vel = v
	ai.xxa *= .98
	ai.zza *= .98
	switch {
	case !m.Alive() || mobImmobile(m):
		ai.jumping, ai.xxa, ai.zza = false, 0, 0
	case !ai.NoAI:
		ai.serverAiStep(m)
	}
	if ai.jumping {
		fluidHeight := ai.waterHeight
		if m.InLava() {
			fluidHeight = ai.lavaHeight
		}
		inWater := m.InWater() && fluidHeight > 0
		threshold := m.FluidJumpThreshold()
		switch {
		case inWater && !(ai.onGround && fluidHeight <= threshold):
			ai.jumpInLiquid(m, false)
		case m.InLava() && !(ai.onGround && ai.lavaHeight <= threshold):
			ai.jumpInLiquid(m, true)
		case (ai.onGround || inWater && fluidHeight <= threshold) && ai.noJumpDelay == 0:
			ai.jumpFromGround(m)
			ai.noJumpDelay = 10
		}
	} else {
		ai.noJumpDelay = 0
	}
	ai.travel(m, mgl64.Vec3{ai.xxa, ai.yya, ai.zza})
}

// serverAiStep ports Mob.serverAiStep.
func (ai *MobAI) serverAiStep(m *Mob) {
	ai.noActionTime++
	ai.sense.tick()
	if (ai.tickCount+int(m.H().UniqueID()&0x7fffffff))%2 != 0 && ai.tickCount > 1 {
		ai.Targets.TickRunning(m, false)
		ai.Goals.TickRunning(m, false)
	} else {
		ai.Targets.Tick(m)
		ai.Goals.Tick(m)
	}
	ai.nav.tickNavigation(m)
	if c, ok := m.Behaviour().(mobCustomStep); ok {
		c.customServerAiStep(m)
	}
	if ai.moveOverride != nil {
		ai.moveOverride.tick(m)
	} else {
		ai.move.tick(m)
	}
	if ai.lookOverride != nil {
		ai.lookOverride.tick(m)
	} else {
		ai.look.tick(m)
	}
	if j, ok := m.Behaviour().(mobJumpControl); ok {
		j.tickJumpControl(m)
	} else {
		ai.jump.tick(m)
	}
}

// jumpInLiquid ports Mob.jumpInLiquid: mobs that float swim up slowly, the
// others push off.
func (ai *MobAI) jumpInLiquid(m *Mob, lava bool) {
	if j, ok := m.Behaviour().(mobJumper); ok && j.jumpInLiquid(m, lava) {
		return
	}
	if ai.nav.CanFloat() {
		m.data.Vel[1] += .04
	} else {
		m.data.Vel[1] += .3
	}
}

// jumpFromGround ports LivingEntity.jumpFromGround.
func (ai *MobAI) jumpFromGround(m *Mob) {
	if j, ok := m.Behaviour().(mobJumper); ok && j.jumpFromGround(m) {
		return
	}
	power := ai.jumpPower(m)
	if power <= 1e-5 {
		return
	}
	m.data.Vel[1] = math.Max(power, m.data.Vel[1])
}

// jumpPower ports LivingEntity.getJumpPower.
func (ai *MobAI) jumpPower(m *Mob) float64 {
	return ai.JumpStrength*blockJumpFactor(m) + jumpBoostPower(m)
}

// jumpBoostPower ports LivingEntity.getJumpBoostPower.
func jumpBoostPower(m *Mob) float64 {
	if l, ok := m.Self.(interface {
		Effect(effect.Type) (effect.Effect, bool)
	}); ok {
		if jump, ok := l.Effect(effect.JumpBoost); ok {
			return .1 * float64(jump.Level())
		}
	}
	return 0
}

// blockJumpFactor ports Entity.getBlockJumpFactor: honey halves jumps.
func blockJumpFactor(m *Mob) float64 {
	if _, honey := m.tx.Block(m.BlockPos()).(block.HoneyBlock); honey {
		return .5
	}
	if _, honey := m.tx.Block(blockPosBelowMovement(m)).(block.HoneyBlock); honey {
		return .5
	}
	return 1
}

// blockPosBelowMovement ports Entity.getBlockPosBelowThatAffectsMyMovement.
func blockPosBelowMovement(m *Mob) cube.Pos {
	return cube.PosFromVec3(mgl64.Vec3{m.data.Pos[0], m.data.Pos[1] - .5000001, m.data.Pos[2]})
}

// blockFriction returns the friction of the block below the mob.
func blockFriction(m *Mob) float64 {
	if f, ok := m.tx.Block(blockPosBelowMovement(m)).(interface{ Friction() float64 }); ok {
		return f.Friction()
	}
	return .6
}

// blockSpeedFactor ports Entity.getBlockSpeedFactor: soul sand and honey
// slow walking.
func blockSpeedFactor(m *Mob) float64 {
	factor := func(b world.Block) float64 {
		switch b.(type) {
		case block.SoulSand, block.HoneyBlock:
			return .4
		}
		return 1
	}
	pos := m.BlockPos()
	if l, ok := m.tx.Liquid(pos); ok && l.LiquidType() == "water" {
		return 1
	}
	if f := factor(m.tx.Block(pos)); f != 1 {
		return f
	}
	return factor(m.tx.Block(blockPosBelowMovement(m)))
}

// travel ports LivingEntity.travel.
func (ai *MobAI) travel(m *Mob, input mgl64.Vec3) {
	if t, ok := m.Behaviour().(mobTraveller); ok && t.travel(m, input) {
		return
	}
	if m.InLiquid() {
		ai.travelInFluid(m, input)
		return
	}
	ai.travelInAir(m, input)
}

// travelRidden ports the travel of LivingEntity.travelRidden: a mob a
// player steers moves by the input at the speed passed, and at a tenth of
// it in the air.
func (ai *MobAI) travelRidden(m *Mob, input mgl64.Vec3, speed float64) {
	ai.speed, ai.ridden = speed, true
	if m.InLiquid() {
		ai.travelInFluid(m, input)
	} else {
		ai.travelInAir(m, input)
	}
	ai.ridden = false
}

// travelFlying ports LivingEntity.travelFlying: the mob moves by input
// without gravity, slowed down by the medium it flies in.
func (ai *MobAI) travelFlying(m *Mob, input mgl64.Vec3, waterSpeed, lavaSpeed, airSpeed float64) {
	speed, slowDown := airSpeed, .91
	switch {
	case m.InWater():
		speed, slowDown = waterSpeed, .8
	case m.InLava():
		speed, slowDown = lavaSpeed, .5
	}
	ai.moveRelative(m, speed, input)
	ai.moveSelf(m, m.data.Vel)
	m.data.Vel = m.data.Vel.Mul(slowDown)
}

// travelInAir ports LivingEntity.travelInAir.
func (ai *MobAI) travelInAir(m *Mob, input mgl64.Vec3) {
	friction := 1.0
	if ai.onGround {
		friction = blockFriction(m)
	}
	speed := ai.flyingSpeed()
	if ai.onGround {
		speed = ai.speed
		if friction > .6 {
			speed = ai.speed * (.21600002 / (friction * friction * friction))
		}
	}
	ai.moveRelative(m, speed, input)
	m.data.Vel = ai.handleOnClimbable(m, m.data.Vel)
	ai.moveSelf(m, m.data.Vel)
	v := m.data.Vel
	if (ai.collidedHorizontally || ai.jumping) && onClimbable(m) {
		v[1] = .2
	}
	if levitation, ok := mobEffect(m, effect.Levitation); ok {
		v[1] += (.05*float64(levitation.Level()) - v[1]) * .2
	} else {
		v[1] -= ai.effectiveGravity(m)
	}
	m.data.Vel = mgl64.Vec3{v[0] * friction * .91, v[1] * .98, v[2] * friction * .91}
}

// flyingSpeed ports LivingEntity.getFlyingSpeed: a mob a player steers
// keeps a tenth of its speed in the air.
func (ai *MobAI) flyingSpeed() float64 {
	if ai.ridden {
		return ai.speed * .1
	}
	return .02
}

// effectiveGravity ports LivingEntity.getEffectiveGravity: slow falling
// eases falls.
func (ai *MobAI) effectiveGravity(m *Mob) float64 {
	if ai.NoGravity {
		return 0
	}
	if m.data.Vel[1] <= 0 {
		if _, ok := mobEffect(m, effect.SlowFalling); ok {
			return math.Min(ai.Gravity, .01)
		}
	}
	return ai.Gravity
}

func mobEffect(m *Mob, t effect.Type) (effect.Effect, bool) {
	if l, ok := m.Self.(interface {
		Effect(effect.Type) (effect.Effect, bool)
	}); ok {
		return l.Effect(t)
	}
	return effect.Effect{}, false
}

// travelInFluid ports LivingEntity.travelInFluid.
func (ai *MobAI) travelInFluid(m *Mob, input mgl64.Vec3) {
	falling := m.data.Vel[1] <= 0
	oldY := m.data.Pos[1]
	gravity := ai.effectiveGravity(m)
	if m.InWater() {
		slowDown := .8
		if s, ok := m.Behaviour().(mobWaterSlowDown); ok {
			slowDown = s.waterSlowDown()
		}
		ai.moveRelative(m, .02, input)
		ai.moveSelf(m, m.data.Vel)
		v := m.data.Vel
		if ai.collidedHorizontally && onClimbable(m) {
			v[1] = .2
		}
		v = mgl64.Vec3{v[0] * slowDown, v[1] * .8, v[2] * slowDown}
		m.data.Vel = fluidFallingAdjusted(gravity, falling, v)
	} else {
		ai.moveRelative(m, .02, input)
		ai.moveSelf(m, m.data.Vel)
		if ai.lavaHeight <= m.FluidJumpThreshold() {
			v := m.data.Vel
			m.data.Vel = fluidFallingAdjusted(gravity, falling, mgl64.Vec3{v[0] * .5, v[1] * .8, v[2] * .5})
		} else {
			m.data.Vel = m.data.Vel.Mul(.5)
		}
		if gravity != 0 {
			m.data.Vel[1] -= gravity / 4
		}
	}
	v := m.data.Vel
	if ai.collidedHorizontally && boxFreeOfLiquid(m, m.BBox().Translate(mgl64.Vec3{v[0], v[1] + .6 - m.data.Pos[1] + oldY, v[2]})) {
		m.data.Vel[1] = .3
	}
}

// fluidFallingAdjusted ports LivingEntity.getFluidFallingAdjustedMovement.
func fluidFallingAdjusted(gravity float64, falling bool, v mgl64.Vec3) mgl64.Vec3 {
	if gravity == 0 {
		return v
	}
	if falling && math.Abs(v[1]-.005) >= .003 && math.Abs(v[1]-gravity/16) < .003 {
		v[1] = -.003
	} else {
		v[1] -= gravity / 16
	}
	return v
}

// boxFreeOfLiquid reports if no block collides with a box and it holds no
// liquid, as Entity.isFree.
func boxFreeOfLiquid(m *Mob, box cube.BBox) bool {
	if !boxFree(m.tx, box) {
		return false
	}
	minX, minY, minZ := int(math.Floor(box.Min()[0])), int(math.Floor(box.Min()[1])), int(math.Floor(box.Min()[2]))
	maxX, maxY, maxZ := int(math.Ceil(box.Max()[0])), int(math.Ceil(box.Max()[1])), int(math.Ceil(box.Max()[2]))
	for x := minX; x < maxX; x++ {
		for y := minY; y < maxY; y++ {
			for z := minZ; z < maxZ; z++ {
				if _, ok := m.tx.Liquid(cube.Pos{x, y, z}); ok {
					return false
				}
			}
		}
	}
	return true
}

// moveRelative ports Entity.moveRelative: input relative to where the mob
// faces is added to its velocity.
func (ai *MobAI) moveRelative(m *Mob, speed float64, input mgl64.Vec3) {
	length := input.LenSqr()
	if length < 1e-7 {
		return
	}
	if length > 1 {
		input = input.Normalize()
	}
	input = input.Mul(speed)
	yaw := m.Yaw() * math.Pi / 180
	sin, cos := math.Sin(yaw), math.Cos(yaw)
	m.data.Vel = m.data.Vel.Add(mgl64.Vec3{input[0]*cos - input[2]*sin, input[1], input[2]*cos + input[0]*sin})
}

// handleOnClimbable ports LivingEntity.handleOnClimbable.
func (ai *MobAI) handleOnClimbable(m *Mob, v mgl64.Vec3) mgl64.Vec3 {
	if !onClimbable(m) {
		return v
	}
	m.data.FallDistance = 0
	return mgl64.Vec3{math.Max(-.15, math.Min(.15, v[0])), math.Max(v[1], -.15), math.Max(-.15, math.Min(.15, v[2]))}
}

// onClimbable ports LivingEntity.onClimbable for mobs: they climb blocks in
// the climbable tag and open trapdoors above ladders, and spiders the walls
// they walk into.
func onClimbable(m *Mob) bool {
	if c, ok := m.Behaviour().(interface{ climbingWall() bool }); ok && c.climbingWall() {
		return true
	}
	pos := m.BlockPos()
	b := m.tx.Block(pos)
	if blockInTag(b, "minecraft:climbable") {
		return true
	}
	if t, ok := b.(block.WoodTrapdoor); ok && t.Open {
		if l, ok := m.tx.Block(pos.Side(cube.FaceDown)).(block.Ladder); ok && l.Facing == t.Facing {
			return true
		}
	}
	return false
}

// moveSelf ports Entity.move for a mob moving itself: it collides with blocks,
// steps up onto blocks as high as its step height and is slowed by the
// block it walks on.
func (ai *MobAI) moveSelf(m *Mob, delta mgl64.Vec3) {
	if ai.NoPhysics {
		m.data.Pos = m.data.Pos.Add(delta)
		ai.collidedHorizontally, ai.collidedVertically, ai.onGround = false, false, false
		return
	}
	movement := ai.collide(m, delta)
	if movement.LenSqr() > 1e-7 || delta.LenSqr()-movement.LenSqr() < 1e-7 {
		m.data.Pos = m.data.Pos.Add(movement)
	}
	xCollision := !floatEqual(delta[0], movement[0])
	zCollision := !floatEqual(delta[2], movement[2])
	ai.collidedHorizontally = xCollision || zCollision
	if math.Abs(delta[1]) > 0 {
		ai.collidedVertically = delta[1] != movement[1]
		ai.onGround = ai.collidedVertically && delta[1] < 0
	}
	v := m.data.Vel
	if xCollision {
		v[0] = 0
	}
	if zCollision {
		v[2] = 0
	}
	if ai.collidedVertically {
		if _, slime := m.tx.Block(blockPosBelowMovement(m)).(block.Slime); slime && delta[1] < 0 && v[1] < 0 {
			v[1] = -v[1]
		} else {
			v[1] = 0
		}
	}
	factor := blockSpeedFactor(m)
	v[0], v[2] = v[0]*factor, v[2]*factor
	m.data.Vel = v
}

// floatEqual ports Mth.equal.
func floatEqual(a, b float64) bool { return math.Abs(b-a) < 1e-5 }

// collide ports Entity.collide: the movement of the mob through the blocks
// around it, stepping up onto those as high as its step height.
func (ai *MobAI) collide(m *Mob, movement mgl64.Vec3) mgl64.Vec3 {
	box := m.BBox()
	step := ai.StepHeight
	colliders := blockBBoxsAround(m.tx, box.Extend(movement).Extend(mgl64.Vec3{0, step}))
	defer func() {
		if colliders != nil {
			blockBBoxPool.Put(colliders[:0])
		}
	}()
	result := movement
	if movement.LenSqr() != 0 {
		result = collideWithBoxes(movement, box, colliders)
	}
	xCollision, yCollision, zCollision := movement[0] != result[0], movement[1] != result[1], movement[2] != result[2]
	groundAfter := yCollision && movement[1] < 0
	if step <= 0 || !(groundAfter || ai.onGround) || !(xCollision || zCollision) {
		return result
	}
	grounded := box
	if groundAfter {
		grounded = box.Translate(mgl64.Vec3{0, result[1]})
	}
	skip := result[1]
	var candidates []float64
	for _, bb := range colliders {
		for _, coord := range [2]float64{bb.Min()[1], bb.Max()[1]} {
			rel := coord - grounded.Min()[1]
			if rel < 0 || rel == skip || rel > step || slices.Contains(candidates, rel) {
				continue
			}
			candidates = append(candidates, rel)
		}
	}
	slices.Sort(candidates)
	for _, height := range candidates {
		stepped := collideWithBoxes(mgl64.Vec3{movement[0], height, movement[2]}, grounded, colliders)
		if stepped[0]*stepped[0]+stepped[2]*stepped[2] > result[0]*result[0]+result[2]*result[2] {
			return stepped.Sub(mgl64.Vec3{0, box.Min()[1] - grounded.Min()[1]})
		}
	}
	return result
}

// collideWithBoxes ports Entity.collideWithShapes: the movement on the Y
// axis first, then on the axis the box moves furthest on.
func collideWithBoxes(movement mgl64.Vec3, box cube.BBox, boxes []cube.BBox) mgl64.Vec3 {
	if len(boxes) == 0 {
		return movement
	}
	var resolved mgl64.Vec3
	axes := [3]int{1, 0, 2}
	if math.Abs(movement[0]) < math.Abs(movement[2]) {
		axes = [3]int{1, 2, 0}
	}
	for _, axis := range axes {
		d := movement[axis]
		if d == 0 {
			continue
		}
		moved := box.Translate(resolved)
		for _, bb := range boxes {
			switch axis {
			case 0:
				d = moved.XOffset(bb, d)
			case 1:
				d = moved.YOffset(bb, d)
			case 2:
				d = moved.ZOffset(bb, d)
			}
		}
		resolved[axis] = d
	}
	return resolved
}

// updateFluids measures how deep the mob stands in water and lava, as
// Entity.updateFluidHeightAndDoFluidPushing measures it.
func (ai *MobAI) updateFluids(m *Mob) {
	ai.waterHeight, ai.lavaHeight = 0, 0
	box := m.BBox().Grow(-.001)
	minX, minY, minZ := int(math.Floor(box.Min()[0])), int(math.Floor(box.Min()[1])), int(math.Floor(box.Min()[2]))
	maxX, maxY, maxZ := int(math.Ceil(box.Max()[0])), int(math.Ceil(box.Max()[1])), int(math.Ceil(box.Max()[2]))
	for x := minX; x < maxX; x++ {
		for y := minY; y < maxY; y++ {
			for z := minZ; z < maxZ; z++ {
				pos := cube.Pos{x, y, z}
				l, ok := m.tx.Liquid(pos)
				if !ok {
					continue
				}
				height := float64(y) + liquidHeight(m.tx, pos, l)
				if height < box.Min()[1] {
					continue
				}
				switch l.LiquidType() {
				case "water":
					ai.waterHeight = math.Max(ai.waterHeight, height-box.Min()[1])
				case "lava":
					ai.lavaHeight = math.Max(ai.lavaHeight, height-box.Min()[1])
				}
			}
		}
	}
}

// liquidHeight ports FlowingFluid.getHeight: a liquid under the same liquid
// fills its block, others amount/9 of it.
func liquidHeight(tx *world.Tx, pos cube.Pos, l world.Liquid) float64 {
	if above, ok := tx.Liquid(pos.Side(cube.FaceUp)); ok && above.LiquidType() == l.LiquidType() {
		return 1
	}
	if l.LiquidFalling() {
		return 8.0 / 9
	}
	return float64(l.LiquidDepth()) / 9
}

// newMobMovement returns the movement of a mob over a tick for its viewers.
func newMobMovement(m *Mob, prevPos, prevVel mgl64.Vec3, prevRot cube.Rotation, prevHead float64) *Movement {
	tx := m.tx
	viewers := tx.Viewers(prevPos)
	return &Movement{v: viewers, release: func() { tx.ReleaseViewers(viewers) }, e: m.Self,
		pos: m.data.Pos, vel: m.data.Vel, dpos: m.data.Pos.Sub(prevPos), dvel: m.data.Vel.Sub(prevVel),
		rot: m.data.Rot, onGround: m.AI.onGround,
		rotChanged: rotationChanged(m.data.Rot, prevRot) || math.Abs(m.AI.headYaw-prevHead) > .01,
	}
}

// riderMovement places a riding mob on its vehicle.
func (ai *MobAI) riderMovement(m *Mob, prevPos mgl64.Vec3, prevRot cube.Rotation, prevHead float64) *Movement {
	tx := m.tx
	vehicle, ok := m.data.Riding.Entity(tx)
	if !ok {
		m.data.Riding = nil
		return newMobMovement(m, prevPos, m.data.Vel, prevRot, prevHead)
	}
	if l, ok := vehicle.(Living); ok && l.Dead() {
		m.data.Riding = nil
	} else if positioner, ok := vehicle.(passengerPositioner); ok {
		m.data.Pos = positioner.PassengerPosition(m.Self)
	}
	m.data.Vel = mgl64.Vec3{}
	ai.onGround = false
	return newMobMovement(m, prevPos, mgl64.Vec3{}, prevRot, prevHead)
}
