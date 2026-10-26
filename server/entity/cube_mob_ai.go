package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// This file ports Java's AbstractCubeMob: slimes and magma cubes, which hop
// the way they face instead of walking paths.

// cubeAI is the state a cube mob keeps beside its MobAI.
type cubeAI struct {
	move        cubeMoveControl
	wasOnGround bool
}

// cubeMob is implemented by the behaviours of cube mobs.
type cubeMob interface {
	cube() *cubeAI
	cubeSize() int
	// cubeMagma reports if the cube is a magma cube.
	cubeMagma() bool
}

func cubeOf(m *Mob) cubeMob { return m.Behaviour().(cubeMob) }

// cubeJumpDelay ports AbstractCubeMob.getJumpDelay, four times as long for
// magma cubes.
func cubeJumpDelay(m *Mob, c cubeMob) int {
	d := int(m.Random().NextInt(20)) + 10
	if c.cubeMagma() {
		d *= 4
	}
	return d
}

// cubeCanDealDamage ports AbstractCubeMob.canDealDamage: the smallest slimes
// are harmless, and cubes that override it, as sulfur cubes, decide it.
func cubeCanDealDamage(c cubeMob) bool {
	if d, ok := c.(interface{ cubeDealsDamage() bool }); ok {
		return d.cubeDealsDamage()
	}
	return c.cubeMagma() || c.cubeSize() > 1
}

// cubeSoundPitch ports AbstractCubeMob.getSoundPitch.
func cubeSoundPitch(m *Mob, c cubeMob) float64 {
	adjust := .8
	if c.cubeSize() <= 1 {
		adjust = 1.4
	}
	r := m.Random()
	return (float64(r.NextFloat()-r.NextFloat())*.2 + 1) * adjust
}

func cubeSound(c cubeMob, name string) string {
	if n, ok := c.(interface{ cubeSoundName(string) string }); ok {
		return n.cubeSoundName(name)
	}
	prefix := "entity.slime."
	if c.cubeMagma() {
		prefix = "entity.magma_cube."
	}
	if c.cubeSize() <= 1 && (name != "jump" || !c.cubeMagma()) {
		name += "_small"
	}
	return prefix + name
}

// registerCubeGoals ports AbstractCubeMob.registerGoals with the goals Slime
// and MagmaCube add.
func registerCubeGoals(m *Mob, c cubeMob) {
	ai := m.AI
	a := c.cube()
	a.move.yRot = 180 * m.Yaw() / math.Pi
	ai.moveOverride = &a.move
	ai.nav.SetCanFloat(true)
	ai.Goals.Add(1, &cubeFloatGoal{goalFlags{GoalJump | GoalMove}})
	ai.Goals.Add(2, &cubeAttackGoal{goalFlags: goalFlags{GoalLook}})
	ai.Goals.Add(4, &cubeRandomDirectionGoal{goalFlags: goalFlags{GoalLook}})
	ai.Goals.Add(5, &cubeKeepOnJumpingGoal{goalFlags{GoalJump | GoalMove}})
	players := NewNearestAttackableTargetGoal(nil, true)
	players.Selector = func(m *Mob, target world.Entity) bool { return math.Abs(target.Position()[1]-m.data.Pos[1]) <= 4 }
	ai.Targets.Add(1, players)
	ai.Targets.Add(3, NewNearestAttackableTypesGoal(entityTypes{"minecraft:iron_golem"}, true))
}

// tickCube runs what AbstractCubeMob does after the AI of a cube mob: it
// squishes when it lands and hurts the players touching it and the iron
// golems it bumps into.
func tickCube(m *Mob, c cubeMob) {
	a := c.cube()
	if onGround := m.OnGround(); onGround && !a.wasOnGround {
		r := m.Random()
		pitch := (float64(r.NextFloat()-r.NextFloat())*.2 + 1) / .8
		m.tx.PlaySound(m.data.Pos, sound.Custom{Name: cubeSound(c, "squish"), Volume: .4 * float64(c.cubeSize()), Pitch: pitch})
	}
	a.wasOnGround = m.OnGround()
	if !cubeCanDealDamage(c) || !m.Alive() {
		return
	}
	box := m.BBox()
	// Player.aiStep touches the entities within its box grown by one block
	// sideways and half a block up and down.
	for p := range m.tx.Players() {
		if spectator(p) || !entityAlive(p) {
			continue
		}
		if p.H().Type().BBox(p).Translate(p.Position()).GrowVec3(mgl64.Vec3{1, .5, 1}).IntersectsWith(box) {
			cubeDealDamage(m, c, p)
		}
	}
	for e := range m.tx.EntitiesWithin(box) {
		if e.H().Type().EncodeEntity() == "minecraft:iron_golem" && e.H().Type().BBox(e).Translate(e.Position()).IntersectsWith(box) {
			cubeDealDamage(m, c, e)
		}
	}
}

// cubeDealDamage ports AbstractCubeMob.dealDamage: the cube hits what it
// reaches and sees, magma cubes two points harder.
func cubeDealDamage(m *Mob, c cubeMob, target world.Entity) {
	l, ok := target.(Living)
	if !ok || !m.WithinMeleeAttackRange(target) || !m.HasLineOfSight(target) {
		return
	}
	d := float64(c.cubeSize())
	if c.cubeMagma() {
		d += 2
	}
	if _, hurt := l.Hurt(d, AttackDamageSource{Attacker: m.Self}); hurt {
		r := m.Random()
		m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "entity.slime.attack", Volume: 1, Pitch: float64(r.NextFloat()-r.NextFloat())*.2 + 1})
	}
}

// splitCube ports AbstractCubeMob.remove: a dying cube larger than the
// smallest splits into two to four cubes of half its size. spawn returns a
// cube of a size.
func splitCube(m *Mob, size int, spawn func(world.EntitySpawnOpts, int) *world.EntityHandle) {
	if size <= 1 {
		return
	}
	offset := m.Width() / 2
	r := m.Random()
	count := 2 + int(r.NextInt(3))
	for i := range count {
		xd := (float64(i%2) - .5) * offset
		zd := (float64(i/2) - .5) * offset
		// Entity.convertTo makes the halves without finalizing their spawn.
		opts := world.EntitySpawnOpts{Position: m.data.Pos.Add(mgl64.Vec3{xd, .5, zd}), Rotation: cube.Rotation{float64(r.NextFloat()) * 360, 0}, SpawnReason: "triggered", SpawnFinalized: true}
		m.tx.AddEntity(spawn(opts, size/2))
	}
}

// cubeMoveControl hops a cube mob the way it faces, ported from Java's
// CubeMobMoveControl.
type cubeMoveControl struct {
	yRot          float64
	jumpDelay     int
	aggressive    bool
	speedModifier float64
	moveTo        bool
}

func (c *cubeMoveControl) setDirection(yRot float64, aggressive bool) {
	c.yRot, c.aggressive = yRot, aggressive
}

func (c *cubeMoveControl) setWantedMovement(speedModifier float64) {
	c.speedModifier, c.moveTo = speedModifier, true
}

func (c *cubeMoveControl) tick(m *Mob) {
	yaw := rotlerp(m.Yaw(), c.yRot, 90)
	m.SetYaw(yaw)
	m.AI.headYaw, m.AI.bodyYaw = yaw, yaw
	if !c.moveTo {
		m.AI.zza = 0
		return
	}
	c.moveTo = false
	speed := c.speedModifier * m.AI.MovementSpeed
	if !m.OnGround() {
		m.SetSpeed(speed)
		return
	}
	m.SetSpeed(speed)
	waited := c.jumpDelay
	c.jumpDelay--
	if waited > 0 {
		m.AI.xxa, m.AI.zza = 0, 0
		m.SetSpeed(0)
		return
	}
	cm := cubeOf(m)
	c.jumpDelay = cubeJumpDelay(m, cm)
	if c.aggressive {
		c.jumpDelay /= 3
	}
	m.AI.jump.Jump()
	m.tx.PlaySound(m.data.Pos, sound.Custom{Name: cubeSound(cm, "jump"), Volume: .4 * float64(cm.cubeSize()), Pitch: cubeSoundPitch(m, cm)})
}

// cubeFloatGoal keeps a cube mob afloat in liquids, ported from Java's
// CubeMobFloatGoal.
type cubeFloatGoal struct{ goalFlags }

func (g *cubeFloatGoal) CanUse(m *Mob) bool { return m.InLiquid() }

func (g *cubeFloatGoal) RequiresUpdateEveryTick() bool { return true }

func (g *cubeFloatGoal) Tick(m *Mob) {
	if m.Random().NextFloat() < .8 {
		m.AI.jump.Jump()
	}
	cubeOf(m).cube().move.setWantedMovement(1.2)
}

// cubeAttackGoal turns a cube mob to its target until it grows tired,
// ported from Java's CubeMobAttackGoal.
type cubeAttackGoal struct {
	goalFlags
	growTiredTimer int
}

func (g *cubeAttackGoal) CanUse(m *Mob) bool {
	target := m.Target()
	return target != nil && m.CanAttack(target)
}

func (g *cubeAttackGoal) Start(*Mob) { g.growTiredTimer = reducedTickDelay(300) }

func (g *cubeAttackGoal) CanContinueToUse(m *Mob) bool {
	if !g.CanUse(m) {
		return false
	}
	g.growTiredTimer--
	return g.growTiredTimer > 0
}

func (g *cubeAttackGoal) RequiresUpdateEveryTick() bool { return true }

func (g *cubeAttackGoal) Tick(m *Mob) {
	if target := m.Target(); target != nil {
		m.LookAt(target, 10, 10)
	}
	c := cubeOf(m)
	c.cube().move.setDirection(m.Yaw(), cubeCanDealDamage(c))
}

// cubeRandomDirectionGoal turns an idle cube mob a random way now and then,
// ported from Java's CubeMobRandomDirectionGoal.
type cubeRandomDirectionGoal struct {
	goalFlags
	chosenDegrees     float64
	nextRandomizeTime int
}

func (g *cubeRandomDirectionGoal) CanUse(m *Mob) bool {
	return m.Target() == nil && (m.OnGround() || m.InLiquid() || mobHasEffect(m, effect.Levitation))
}

func (g *cubeRandomDirectionGoal) Tick(m *Mob) {
	if g.nextRandomizeTime--; g.nextRandomizeTime <= 0 {
		r := m.Random()
		g.nextRandomizeTime = adjustedTickDelay(g, 40+int(r.NextInt(60)))
		g.chosenDegrees = float64(r.NextInt(360))
	}
	cubeOf(m).cube().move.setDirection(g.chosenDegrees, false)
}

// cubeKeepOnJumpingGoal keeps a cube mob hopping, ported from Java's
// CubeMobKeepOnJumpingGoal.
type cubeKeepOnJumpingGoal struct{ goalFlags }

func (g *cubeKeepOnJumpingGoal) CanUse(m *Mob) bool { return !isPassenger(m.Self) }

func (g *cubeKeepOnJumpingGoal) Tick(m *Mob) { cubeOf(m).cube().move.setWantedMovement(1) }

// mobHasEffect reports if a mob has an effect of a type.
func mobHasEffect(m *Mob, t effect.Type) bool {
	l, ok := m.Self.(interface {
		Effect(effect.Type) (effect.Effect, bool)
	})
	if !ok {
		return false
	}
	_, has := l.Effect(t)
	return has
}
