package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// This file ports the goals and heads of Java's WitherBoss.

func (s *witherState) mobAI() *MobAI { return &s.ai }

// Tick runs a tick of the wither: its death, the steering of WitherBoss.aiStep
// towards the target of its middle head and its MobAI.
func (s *witherState) Tick(e *Ent, tx *world.Tx) *Movement {
	w := &Wither{Ent: e}
	if s.dead {
		s.death++
		if s.death >= 220 {
			_ = e.CloseIn(tx)
		}
		e.data.Vel = mgl64.Vec3{}
		return nil
	}
	ai := &s.ai
	ai.MovementSpeed, ai.FlyingSpeed, ai.FollowRange, ai.EyeHeight = s.speed, witherBaseMovementSpeed, 40, 2.975
	// WitherBoss.aiStep: the wither rises to the height of its target, and
	// five blocks above it until it is powered, and closes in on it.
	v := e.data.Vel
	v[1] *= .6
	if t := s.headTarget(tx, 0); t != nil {
		tp := t.Position()
		if e.data.Pos[1] < tp[1] || !w.Powered() && e.data.Pos[1] < tp[1]+5 {
			v[1] = math.Max(0, v[1])
			v[1] += .3 - v[1]*float64(float32(.6))
		}
		if d := (mgl64.Vec3{tp[0] - e.data.Pos[0], 0, tp[2] - e.data.Pos[2]}); d.LenSqr() > 9 {
			d = d.Normalize()
			v[0] += d[0]*.3 - v[0]*.6
			v[2] += d[2]*.3 - v[2]*.6
		}
	}
	e.data.Vel = v
	if v[0]*v[0]+v[2]*v[2] > .05 {
		r := e.data.Rot
		r[0] = math.Atan2(v[2], v[0])*180/math.Pi - 90
		e.data.Rot = r
	}
	mov := ai.Tick(e, w, s.registerGoals)
	w.updateBossBar()
	return mov
}

// registerGoals ports WitherBoss.registerGoals and the navigation and move
// control of its constructor: the wither flies, and shoots its middle head
// at its target.
func (s *witherState) registerGoals(m *Mob) {
	ai := m.AI
	ai.nav.SetFlying()
	ai.nav.SetCanFloat(true)
	ai.nav.SetCanOpenDoors(false)
	ai.moveOverride = &flyingMoveControl{maxTurn: 10}
	ai.Goals.Add(0, &witherDoNothingGoal{goalFlags: goalFlags{GoalMove | GoalJump | GoalLook}, wither: s})
	ai.Goals.Add(2, NewRangedAttackGoal(1, 40, 20, func(m *Mob, target world.Entity, _ float64) {
		s.performRangedAttackAt(m, 0, target)
	}))
	stroll := NewRandomStrollGoal(1)
	stroll.Position = waterAvoidingRandomFlyingPos
	ai.Goals.Add(5, stroll)
	ai.Goals.Add(6, NewLookAtPlayerGoal(8))
	ai.Goals.Add(7, NewRandomLookAroundGoal())
	ai.Targets.Add(1, NewHurtByTargetGoal())
	targets := NewNearestAttackableTargetGoal(witherTargetable, false)
	ai.Targets.Add(2, targets)
}

// witherTargetable ports WitherBoss.LIVING_ENTITY_SELECTOR: the wither attacks
// any living thing but its undead friends.
func witherTargetable(e world.Entity) bool {
	_, living := e.(Living)
	return living && !witherFriend(e)
}

// witherFriend reports if the entity is in #wither_friends, the undead.
func witherFriend(e world.Entity) bool {
	switch id := e.H().Type().EncodeEntity(); id {
	case "minecraft:wither", "minecraft:phantom", "minecraft:zombie_horse", "minecraft:skeleton_horse", "minecraft:zombified_piglin",
		"minecraft:zoglin", "minecraft:zombie_nautilus", "minecraft:camel_husk":
		return true
	default:
		return hostileUndead(id)
	}
}

// customServerAiStep ports WitherBoss.customServerAiStep: a wither being
// summoned heals up and blows up when it is done. Then its side heads pick
// targets of their own nearby and shoot at them, now and then at random
// spots on normal and hard difficulty, it breaks the blocks around it a
// second after it is hurt, and it heals a point a second.
func (s *witherState) customServerAiStep(m *Mob) {
	w := m.Self.(*Wither)
	tx := m.tx
	// WitherBoss.checkDespawn keeps the wither ever active.
	m.AI.noActionTime = 0
	if s.invulnerable > 0 {
		s.invulnerable--
		if s.invulnerable <= 0 {
			eye := mgl64.Vec3{m.data.Pos[0], m.EyeY(), m.data.Pos[2]}
			block.ExplosionConfig{NoBlockDamage: mobGriefingDisabled(tx)}.Explode(tx, world.NewEntityExplosionSource(witherExplosionSource{w, eye}, 7))
			tx.PlayGlobalSound(m.data.Pos, sound.Custom{Name: "mob.wither.spawn", Volume: 1, Pitch: 1})
		}
		m.updateState()
		if m.AI.tickCount%10 == 0 {
			w.Heal(10, nil)
		}
		return
	}
	r := m.Random()
	tick := m.AI.tickCount
	for i := 1; i < 3; i++ {
		if tick < s.nextHeadUpdate[i-1] {
			continue
		}
		s.nextHeadUpdate[i-1] = tick + 10 + int(r.NextInt(10))
		if d := tx.World().Difficulty(); d == world.DifficultyNormal || d == world.DifficultyHard {
			if s.idleHeadUpdates[i-1]++; s.idleHeadUpdates[i-1]-1 > 15 {
				pos := m.data.Pos
				x := pos[0] - 10 + r.NextDouble()*20
				y := pos[1] - 5 + r.NextDouble()*10
				z := pos[2] - 10 + r.NextDouble()*20
				s.performRangedAttack(m, i+1, mgl64.Vec3{x, y, z}, true)
				s.idleHeadUpdates[i-1] = 0
			}
		}
		if t := s.headTarget(tx, i); t != nil {
			if entityAlive(t) && m.CanAttack(t) && m.DistanceToSqr(t) <= 900 && m.HasLineOfSight(t) {
				s.performRangedAttackAt(m, i+1, t)
				s.nextHeadUpdate[i-1] = tick + 40 + int(r.NextInt(20))
				s.idleHeadUpdates[i-1] = 0
			} else {
				s.setHeadTarget(m, i, nil)
			}
			continue
		}
		c := forCombat()
		c.rangeLimit, c.selector = 20, func(_ *Mob, e world.Entity) bool { return witherTargetable(e) }
		var candidates []world.Entity
		for e := range tx.EntitiesWithin(m.BBox().GrowVec3(mgl64.Vec3{20, 8, 20})) {
			if c.test(m, e) {
				candidates = append(candidates, e)
			}
		}
		if len(candidates) > 0 {
			s.setHeadTarget(m, i, candidates[r.NextInt(int32(len(candidates)))])
		}
	}
	s.setHeadTarget(m, 0, m.Target())
	if s.destroyBlocksTick > 0 {
		if s.destroyBlocksTick--; s.destroyBlocksTick == 0 && !mobGriefingDisabled(tx) {
			s.destroyBlocks(m)
		}
	}
	if tick%20 == 0 {
		w.Heal(1, nil)
	}
}

// destroyBlocks ports the block breaking of WitherBoss.customServerAiStep:
// every block around the wither as high as it is breaks, but those in
// #wither_immune.
func (s *witherState) destroyBlocks(m *Mob) {
	width := int(math.Floor(m.Width()/2 + 1))
	height := int(math.Floor(m.Height()))
	base := m.BlockPos()
	destroyed := false
	for x := -width; x <= width; x++ {
		for y := 0; y <= height; y++ {
			for z := -width; z <= width; z++ {
				pos := base.Add(cube.Pos{x, y, z})
				b := m.tx.Block(pos)
				if isAir(b) || witherImmune(b) {
					continue
				}
				breakBlockDropping(m.tx, pos)
				destroyed = true
			}
		}
	}
	if destroyed {
		m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "mob.wither.break_block", Volume: 1, Pitch: 1})
	}
}

// witherImmune reports if the block is in #wither_immune.
func witherImmune(b world.Block) bool {
	name, _ := b.EncodeBlock()
	switch name {
	case "minecraft:barrier", "minecraft:bedrock", "minecraft:end_portal", "minecraft:end_portal_frame", "minecraft:end_gateway",
		"minecraft:command_block", "minecraft:repeating_command_block", "minecraft:chain_command_block", "minecraft:structure_block",
		"minecraft:jigsaw", "minecraft:moving_block", "minecraft:light_block", "minecraft:reinforced_deepslate", "minecraft:test_block",
		"minecraft:test_instance_block", "minecraft:structure_void":
		return true
	}
	return false
}

// headTarget returns the target of a head, 0 being the middle one.
func (s *witherState) headTarget(tx *world.Tx, head int) world.Entity {
	if s.heads[head] == nil {
		return nil
	}
	e, ok := s.heads[head].Entity(tx)
	if !ok {
		return nil
	}
	return e
}

// setHeadTarget changes the target of a head and shows viewers.
func (s *witherState) setHeadTarget(m *Mob, head int, target world.Entity) {
	var h *world.EntityHandle
	if target != nil {
		h = target.H()
	}
	if s.heads[head] != h {
		s.heads[head] = h
		m.updateState()
	}
}

// headPos ports WitherBoss.getHeadX, getHeadY and getHeadZ: the middle head
// sits three blocks up, the side heads 2.2 blocks up and 1.3 to either side.
func headPos(m *Mob, head int) mgl64.Vec3 {
	pos := m.data.Pos
	if head <= 0 {
		return pos.Add(mgl64.Vec3{0, 3})
	}
	angle := float64(float32((m.AI.bodyYaw + 180*float64(head-1)) * math.Pi / 180))
	return mgl64.Vec3{pos[0] + math.Cos(angle)*1.3, pos[1] + 2.2, pos[2] + math.Sin(angle)*1.3}
}

// performRangedAttackAt ports WitherBoss.performRangedAttack at an entity:
// the head shoots at the middle of its target, the middle head now and then
// a blue, dangerous skull.
func (s *witherState) performRangedAttackAt(m *Mob, head int, target world.Entity) {
	tp := target.Position()
	tp[1] += (entityEyeY(target) - tp[1]) * .5
	s.performRangedAttack(m, head, tp, head == 0 && m.Random().NextFloat() < .001)
}

// performRangedAttack ports WitherBoss.performRangedAttack: a head fires a
// wither skull at a spot.
func (s *witherState) performRangedAttack(m *Mob, head int, at mgl64.Vec3, dangerous bool) {
	m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "mob.wither.shoot", Volume: 1, Pitch: 1})
	from := headPos(m, head)
	dir := at.Sub(from)
	if dir.LenSqr() == 0 {
		return
	}
	m.tx.AddEntity(NewWitherSkull(world.EntitySpawnOpts{Position: from, Velocity: dir.Normalize().Mul(.1)}, m.Self, dangerous))
}

// witherDoNothingGoal ports WitherBoss.WitherDoNothingGoal: a wither being
// summoned does nothing else.
type witherDoNothingGoal struct {
	goalFlags
	wither *witherState
}

func (g *witherDoNothingGoal) CanUse(*Mob) bool { return g.wither.invulnerable > 0 }

// witherExplosionSource is the wither blowing up at its eyes when it is
// summoned.
type witherExplosionSource struct {
	*Wither
	eye mgl64.Vec3
}

func (w witherExplosionSource) Position() mgl64.Vec3 { return w.eye }

// Powered reports if the wither is below half health, when arrows bounce off
// its armour.
func (w *Wither) Powered() bool { return w.Health() <= w.MaxHealth()/2 }

// InvulnerableTicks returns the ticks left of the summoning of the wither.
func (w *Wither) InvulnerableTicks() int { return w.state().invulnerable }

// HeadTargets returns the targets of the middle and side heads of the
// wither, nil for heads without one.
func (w *Wither) HeadTargets() [3]*world.EntityHandle { return w.state().heads }
