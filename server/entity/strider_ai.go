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

// This file ports the goals and movement of Java's Strider.

// Tick runs a tick of the strider as Strider.tick does: it sounds happy
// while tempted and scared while panicking, shivers out of lava, which slows
// it down by a third, and runs its MobAI.
func (b *striderState) Tick(e *Ent, tx *world.Tx) *Movement {
	s := &Strider{Ent: e}
	m := b.ai.Mob(e, s)
	if b.ai.started {
		r := m.Random()
		if b.tempt != nil && b.tempt.Running() && r.NextInt(140) == 0 {
			b.makeSound(m, "entity.strider.happy")
		} else if b.panicking() && r.NextInt(60) == 0 {
			b.makeSound(m, "entity.strider.retreat")
		}
	}
	if !b.ai.NoAI {
		pos := cube.PosFromVec3(e.data.Pos)
		on := cube.PosFromVec3(e.data.Pos.Sub(mgl64.Vec3{0, .2}))
		warm := striderWarmBlock(tx.Block(pos)) || striderWarmBlock(tx.Block(on)) || b.ai.lavaHeight > 0
		if !warm && e.data.Riding != nil {
			if v, ok := e.data.Riding.Entity(tx); ok {
				if mount, ok := v.(*Strider); ok && !mount.b().suffocating {
					warm = true
				}
			}
		}
		if suffocating := !warm; suffocating != b.suffocating {
			// Viewers see a suffocating strider shiver.
			b.suffocating = suffocating
			e.updateState()
		}
	}
	// Strider.SUFFOCATING_MODIFIER takes a third off the speed of a strider
	// out of lava.
	b.ai.MovementSpeed = b.speed
	if b.suffocating {
		b.ai.MovementSpeed = b.speed * .66
	}
	b.ai.EyeHeight = 1.445
	if b.age < 0 {
		b.ai.EyeHeight = .4375
	}
	return animalTick(e, tx, s, &b.animalAI, func(m *Mob) { b.registerGoals(m, s) })
}

// striderWarmBlock reports if b is in #strider_warm_blocks, which holds
// lava alone.
func striderWarmBlock(b world.Block) bool {
	_, ok := b.(block.Lava)
	return ok
}

// makeSound plays a sound of the strider as Mob.makeSound does.
func (b *striderState) makeSound(m *Mob, name string) {
	r := m.Random()
	pitch := (r.NextFloat()-r.NextFloat())*.2 + 1
	if b.age < 0 {
		pitch = (r.NextFloat()-r.NextFloat())*.2 + 1.5
	}
	m.tx.PlaySound(m.data.Pos, sound.Custom{Name: name, Volume: 1, Pitch: float64(pitch)})
}

// registerGoals ports Strider.registerGoals and the navigation of its
// constructor: a strider walks through lava and fire as it walks on land,
// but never into water.
func (b *striderState) registerGoals(m *Mob, s *Strider) {
	ai := m.AI
	ai.SetPathfindingMalus(PathWater, -1)
	ai.SetPathfindingMalus(PathLava, 0)
	ai.SetPathfindingMalus(PathFireInNeighbour, 0)
	ai.SetPathfindingMalus(PathFire, 0)
	// StriderPathNavigation: lava is a stable destination too.
	ai.nav.StableDestination = func(tx *world.Tx, pos cube.Pos) bool {
		_, lava := tx.Block(pos).(block.Lava)
		return lava || ai.nav.groundStable(tx, pos)
	}
	b.panic = NewPanicGoal(1.65)
	ai.Goals.Add(1, b.panic)
	ai.Goals.Add(2, NewBreedGoal(1))
	b.tempt = NewTemptGoal(1.4, func(st item.Stack) bool {
		return s.breedingFood(st) || itemNamed(st, "minecraft:warped_fungus_on_a_stick")
	}, false)
	ai.Goals.Add(3, b.tempt)
	ai.Goals.Add(4, &striderGoToLavaGoal{goalFlags: goalFlags{GoalMove | GoalJump}, moveToBlock: moveToBlock{targetBlock: true, recalcInterval: 20}})
	ai.Goals.Add(5, NewFollowParentGoal(1))
	stroll := NewRandomStrollGoal(1)
	stroll.Interval = 60
	ai.Goals.Add(7, stroll)
	ai.Goals.Add(8, NewLookAtPlayerGoal(8))
	ai.Goals.Add(8, NewRandomLookAroundGoal())
	lookAtStriders := NewLookAtPlayerGoal(8)
	lookAtStriders.IsType = func(e world.Entity) bool { _, ok := e.(*Strider); return ok }
	ai.Goals.Add(9, lookAtStriders)
}

// walkTargetValue ports Strider.getWalkTargetValue: a strider strolls to
// lava, and once in lava nowhere else.
func (b *striderState) walkTargetValue(m *Mob, pos cube.Pos) float64 {
	if l, ok := m.tx.Liquid(pos); ok && l.LiquidType() == "lava" {
		return 10
	}
	if m.InLava() {
		return math.Inf(-1)
	}
	return 0
}

// canStandOnFluid ports Strider.canStandOnFluid: striders walk on lava.
func (b *striderState) canStandOnFluid(l world.Liquid) bool { return l.LiquidType() == "lava" }

// travel ports LivingEntity.travelRidden for a strider a player steers with
// a warped fungus on a stick: Strider.tickRidden turns it the way the player
// looks, and it walks forward at 0.55 times its speed, 0.35 times while it
// shivers, faster while boosted.
func (b *striderState) travel(m *Mob, _ mgl64.Vec3) bool {
	c := (&Strider{Ent: m.Ent}).controller(m.tx)
	if c == nil || !m.Alive() {
		return false
	}
	rot := c.Rotation()
	m.data.Rot = cube.Rotation{rot.Yaw(), rot.Pitch() * .5}
	m.AI.headYaw, m.AI.bodyYaw = rot.Yaw(), rot.Yaw()
	b.steering.tickBoost()
	factor := .55
	if b.suffocating {
		factor = .35
	}
	m.AI.travelRidden(m, mgl64.Vec3{0, 0, 1}, float64(float32(m.AI.MovementSpeed*factor*b.steering.boostFactor())))
	return true
}

// afterAiStep ports what follows LivingEntity.aiStep for a strider: water
// and rain hurt it, and Strider.floatStrider makes it stand on lava it is
// above the surface of and float up through lava it sank into.
func (b *striderState) afterAiStep(m *Mob) {
	tickWaterSensitive(m)
	if b.dead || !m.InLava() {
		return
	}
	pos := m.BlockPos()
	above, lavaAbove := m.tx.Liquid(pos.Side(cube.FaceUp))
	lavaAbove = lavaAbove && above.LiquidType() == "lava"
	if m.data.Pos[1] > float64(pos[1])+.5-1e-5 && !lavaAbove {
		m.AI.onGround = true
		return
	}
	m.data.Vel = m.data.Vel.Mul(.5).Add(mgl64.Vec3{0, .05})
}

// striderGoToLavaGoal ports Strider.StriderGoToLavaGoal, a MoveToBlockGoal: a
// strider out of lava walks into lava nearby with room above it.
type striderGoToLavaGoal struct {
	goalFlags
	moveToBlock
}

func (g *striderGoToLavaGoal) valid(m *Mob, pos cube.Pos) bool {
	if _, lava := m.tx.Block(pos).(block.Lava); !lava {
		return false
	}
	above := pos.Side(cube.FaceUp)
	return pathfindableLand(m.tx, above, m.tx.Block(above))
}

func (g *striderGoToLavaGoal) CanUse(m *Mob) bool {
	return !m.InLava() && g.moveToBlock.canUse(m, 8, 2, g.valid)
}

func (g *striderGoToLavaGoal) CanContinueToUse(m *Mob) bool {
	return !m.InLava() && g.valid(m, g.pos)
}

func (g *striderGoToLavaGoal) Start(m *Mob) { g.moveToBlock.start(m, 1) }

func (g *striderGoToLavaGoal) RequiresUpdateEveryTick() bool { return true }

func (g *striderGoToLavaGoal) Tick(m *Mob) { g.moveToBlock.tick(m, 1) }
