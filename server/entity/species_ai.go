package entity

import (
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// This file holds the goals of the species mobs driven by a MobAI, ported
// from the registerGoals methods of their Java classes.

func (s *speciesState) mobAI() *MobAI { return &s.ai }

// speciesSelf returns the entity of a species mob.
func speciesSelf(e *Ent, k speciesKind) world.Entity {
	m := &speciesMob{Ent: e, kind: k}
	switch k {
	case spiderSpecies:
		return &Spider{m}
	case caveSpiderSpecies:
		return &CaveSpider{m}
	case creeperSpecies:
		return &Creeper{m}
	case endermanSpecies:
		return &Enderman{m}
	case silverfishSpecies:
		return &Silverfish{m}
	case endermiteSpecies:
		return &Endermite{m}
	}
	return &Slime{m}
}

// aiTick runs the MobAI of a species mob.
func (s *speciesState) aiTick(e *Ent, tx *world.Tx) *Movement {
	ai := &s.ai
	ai.MovementSpeed = s.speed
	ai.FollowRange = 16
	switch s.kind {
	case creeperSpecies:
		ai.EyeHeight = 1.445
	case spiderSpecies:
		ai.EyeHeight = .65
	case caveSpiderSpecies:
		ai.EyeHeight = .45
	case silverfishSpecies, endermiteSpecies:
		ai.EyeHeight = .13
	case endermanSpecies:
		ai.EyeHeight, ai.FollowRange, ai.StepHeight = 2.55, 64, 1
		if s.creepy {
			ai.MovementSpeed += endermanSpeedBonus
		}
	case slimeSpecies:
		ai.EyeHeight = .325 * float64(s.size)
	}
	self := speciesSelf(e, s.kind)
	if s.kind == creeperSpecies && s.tickSwell(e, tx) {
		return nil
	}
	if s.kind == endermanSpecies {
		s.updatePersistentAnger(ai.Mob(e, self))
	}
	mov := ai.Tick(e, self, s.registerGoals)
	if s.kind == endermanSpecies && !s.dead {
		tickWaterSensitive(ai.Mob(e, self))
	}
	if s.kind == slimeSpecies && !s.dead {
		tickCube(ai.Mob(e, self), s)
	}
	if s.kind == spiderSpecies || s.kind == caveSpiderSpecies {
		if climbing := ai.collidedHorizontally; climbing != s.climbing {
			s.climbing = climbing
			e.updateState()
		}
	}
	return mov
}

func (s *speciesState) cube() *cubeAI   { return &s.cubeAI }
func (s *speciesState) cubeSize() int   { return s.size }
func (s *speciesState) cubeMagma() bool { return false }

// jumpFromGround ports Slime.jumpFromGround: a slime jumps as high whatever
// moves it.
func (s *speciesState) jumpFromGround(m *Mob) bool {
	if s.kind != slimeSpecies {
		return false
	}
	m.data.Vel[1] = m.AI.jumpPower(m)
	return true
}

func (s *speciesState) jumpInLiquid(*Mob, bool) bool { return false }

// tickSwell ports the swelling of Creeper.tick and reports if the creeper
// exploded.
func (s *speciesState) tickSwell(e *Ent, tx *world.Tx) bool {
	if s.ignited {
		s.setSwellDir(e, 1)
	}
	if s.swellDir > 0 && s.swell == 0 {
		tx.PlaySound(e.Position(), sound.Custom{Name: "entity.creeper.primed", Volume: 1, Pitch: .5})
	}
	s.swell = max(0, s.swell+s.swellDir)
	if s.swell < s.fuse {
		return false
	}
	s.swell = s.fuse
	m := &speciesMob{Ent: e, kind: s.kind}
	power := 3.0
	if s.charged {
		power = 6
	}
	(block.ExplosionConfig{SpawnFire: false, NoBlockDamage: mobGriefingDisabled(tx)}).Explode(tx, world.NewEntityExplosionSource(m, power))
	m.kill()
	return true
}

// setSwellDir sets whether the creeper swells or shrinks, which clients see
// as its fuse lit.
func (s *speciesState) setSwellDir(e *Ent, dir int) {
	if s.swellDir == dir {
		return
	}
	lit := s.swellDir > 0
	s.swellDir = dir
	if (dir > 0) != lit {
		e.updateState()
	}
}

// Ignited reports if the creeper swells to explode.
func (m *speciesMob) Ignited() bool {
	s := m.s()
	return s.kind == creeperSpecies && (s.swellDir > 0 || s.ignited)
}

// FuseTicks returns the ticks the creeper takes to explode.
func (m *speciesMob) FuseTicks() int { return m.s().fuse }

// Powered reports if the creeper is charged.
func (m *speciesMob) Powered() bool { return m.kind == creeperSpecies && m.s().charged }

// WallClimbing reports if the spider climbs a wall.
func (m *speciesMob) WallClimbing() bool {
	return (m.kind == spiderSpecies || m.kind == caveSpiderSpecies) && m.s().climbing
}

// climbing reports if the spider climbs, which Spider.onClimbable returns.
func (s *speciesState) climbingWall() bool {
	return (s.kind == spiderSpecies || s.kind == caveSpiderSpecies) && s.climbing
}

// registerGoals adds the goals of the mob, as the registerGoals of its Java
// class.
func (s *speciesState) registerGoals(m *Mob) {
	ai := m.AI
	switch s.kind {
	case creeperSpecies:
		ai.Goals.Add(1, NewFloatGoal(m))
		ai.Goals.Add(2, &swellGoal{goalFlags: goalFlags{GoalMove}})
		ai.Goals.Add(3, NewAvoidEntityGoal(entityIs("minecraft:ocelot"), 6, 1, 1.2))
		ai.Goals.Add(3, NewAvoidEntityGoal(entityIs("minecraft:cat"), 6, 1, 1.2))
		ai.Goals.Add(4, NewMeleeAttackGoal(1, false))
		ai.Goals.Add(5, NewWaterAvoidingRandomStrollGoal(.8))
		ai.Goals.Add(6, NewLookAtPlayerGoal(8))
		ai.Goals.Add(6, NewRandomLookAroundGoal())
		ai.Targets.Add(1, NewNearestAttackableTargetGoal(nil, true))
		ai.Targets.Add(2, NewHurtByTargetGoal())
	case endermanSpecies:
		s.registerEndermanGoals(m)
	case slimeSpecies:
		registerCubeGoals(m, s)
	case spiderSpecies, caveSpiderSpecies:
		ai.nav.wallClimber = true
		ai.Goals.Add(1, NewFloatGoal(m))
		armadillos := NewAvoidEntityGoal(entityIs("minecraft:armadillo"), 6, 1, 1.2)
		armadillos.Predicate = func(e world.Entity) bool {
			scared, ok := e.(interface{ Scared() bool })
			return !ok || !scared.Scared()
		}
		ai.Goals.Add(2, armadillos)
		ai.Goals.Add(3, &LeapAtTargetGoal{goalFlags: goalFlags{GoalJump | GoalMove}, YD: .4})
		ai.Goals.Add(4, &spiderAttackGoal{NewMeleeAttackGoal(1, true)})
		ai.Goals.Add(5, NewWaterAvoidingRandomStrollGoal(.8))
		ai.Goals.Add(6, NewLookAtPlayerGoal(8))
		ai.Goals.Add(6, NewRandomLookAroundGoal())
		ai.Targets.Add(1, NewHurtByTargetGoal())
		ai.Targets.Add(2, &spiderTargetGoal{NewNearestAttackableTargetGoal(nil, true)})
		ai.Targets.Add(3, &spiderTargetGoal{NewNearestAttackableTargetGoal(entityIs("minecraft:iron_golem"), true)})
	case silverfishSpecies:
		if s.friends == nil {
			s.friends = &silverfishWakeUpFriendsGoal{}
		}
		ai.Goals.Add(1, NewFloatGoal(m))
		ai.Goals.Add(3, s.friends)
		ai.Goals.Add(4, NewMeleeAttackGoal(1, false))
		ai.Goals.Add(5, newSilverfishMergeWithStoneGoal())
		hurt := NewHurtByTargetGoal()
		hurt.AlertOthers = true
		ai.Targets.Add(1, hurt)
		ai.Targets.Add(2, NewNearestAttackableTargetGoal(nil, true))
	case endermiteSpecies:
		ai.Goals.Add(1, NewFloatGoal(m))
		ai.Goals.Add(2, NewMeleeAttackGoal(1, false))
		ai.Goals.Add(3, NewWaterAvoidingRandomStrollGoal(1))
		ai.Goals.Add(7, NewLookAtPlayerGoal(8))
		ai.Goals.Add(8, NewRandomLookAroundGoal())
		hurt := NewHurtByTargetGoal()
		hurt.AlertOthers = true
		ai.Targets.Add(1, hurt)
		ai.Targets.Add(2, NewNearestAttackableTargetGoal(nil, true))
	}
}

// walkTargetValue ports Monster.getWalkTargetValue, and that of Silverfish
// and Endermite, which like stone and the dark as monsters.
func (s *speciesState) walkTargetValue(m *Mob, pos cube.Pos) float64 {
	if s.kind == silverfishSpecies {
		if infestedHost(m.tx.Block(pos.Side(cube.FaceDown))) != nil {
			return 10
		}
	}
	return monsterWalkTarget(m, pos)
}

// doHurtTarget ports Mob.doHurtTarget with the poison of CaveSpider.
func (s *speciesState) doHurtTarget(m *Mob, target world.Entity) bool {
	l, ok := target.(Living)
	if !ok {
		return false
	}
	if _, hurt := l.Hurt(s.attack, AttackDamageSource{Attacker: m.Self}); !hurt {
		return false
	}
	l.KnockBack(m.data.Pos, .4, .4)
	if s.kind == caveSpiderSpecies {
		switch m.tx.World().Difficulty() {
		case world.DifficultyNormal:
			l.AddEffect(effect.New(effect.Poison, 1, 7*time.Second))
		case world.DifficultyHard:
			l.AddEffect(effect.New(effect.Poison, 1, 15*time.Second))
		}
	}
	return true
}

// lightLevelDependentMagicValue ports
// Entity.getLightLevelDependentMagicValue: the brightness at the eyes.
func lightLevelDependentMagicValue(m *Mob) float64 {
	return pathfindingLightCost(m.tx, cube.PosFromVec3(mgl64.Vec3{m.data.Pos[0], m.EyeY(), m.data.Pos[2]})) + .5
}

// swellGoal makes a creeper swell near its target, ported from Java's
// SwellGoal.
type swellGoal struct {
	goalFlags
	target *world.EntityHandle
}

func creeperState(m *Mob) *speciesState { return m.Behaviour().(*speciesState) }

func (g *swellGoal) CanUse(m *Mob) bool {
	target := m.Target()
	return creeperState(m).swellDir > 0 || target != nil && entityAlive(target) && m.DistanceToSqr(target) < 9
}

func (g *swellGoal) Start(m *Mob) {
	m.AI.nav.Stop()
	if t := m.Target(); t != nil {
		g.target = t.H()
	}
}

func (g *swellGoal) Stop(*Mob) { g.target = nil }

func (g *swellGoal) RequiresUpdateEveryTick() bool { return true }

func (g *swellGoal) Tick(m *Mob) {
	s := creeperState(m)
	var target world.Entity
	if g.target != nil {
		target, _ = g.target.Entity(m.tx)
	}
	switch {
	case target == nil || !entityAlive(target), m.DistanceToSqr(target) > 49, !m.HasLineOfSight(target):
		s.setSwellDir(m.Ent, -1)
	default:
		s.setSwellDir(m.Ent, 1)
	}
}

// LeapAtTargetGoal makes a mob leap at its target, ported from Java's
// LeapAtTargetGoal.
type LeapAtTargetGoal struct {
	goalFlags
	YD     float64
	target *world.EntityHandle
}

func (g *LeapAtTargetGoal) CanUse(m *Mob) bool {
	if isVehicle(m.Self) {
		return false
	}
	target := m.Target()
	if target == nil {
		return false
	}
	d := m.DistanceToSqr(target)
	if d < 4 || d > 16 || !m.OnGround() {
		return false
	}
	if m.Random().NextInt(int32(reducedTickDelay(5))) != 0 {
		return false
	}
	g.target = target.H()
	return true
}

func (g *LeapAtTargetGoal) CanContinueToUse(m *Mob) bool { return !m.OnGround() }

func (g *LeapAtTargetGoal) Start(m *Mob) {
	target, ok := g.target.Entity(m.tx)
	if !ok {
		return
	}
	v := m.data.Vel
	d := target.Position().Sub(m.data.Pos)
	d[1] = 0
	if d.LenSqr() > 1e-7 {
		d = d.Normalize().Mul(.4).Add(v.Mul(.2))
	}
	m.data.Vel = mgl64.Vec3{d[0], g.YD, d[2]}
}

// spiderAttackGoal is Java's Spider.SpiderAttackGoal: a melee attack a
// spider gives up in the light.
type spiderAttackGoal struct{ *MeleeAttackGoal }

func (g *spiderAttackGoal) CanUse(m *Mob) bool {
	return g.MeleeAttackGoal.CanUse(m) && !isVehicle(m.Self)
}

func (g *spiderAttackGoal) CanContinueToUse(m *Mob) bool {
	if lightLevelDependentMagicValue(m) >= .5 && m.Random().NextInt(100) == 0 {
		m.SetTarget(nil)
		return false
	}
	return g.MeleeAttackGoal.CanContinueToUse(m)
}

// spiderTargetGoal is Java's Spider.SpiderTargetGoal: spiders only pick
// targets in the dark.
type spiderTargetGoal struct{ *NearestAttackableTargetGoal }

func (g *spiderTargetGoal) CanUse(m *Mob) bool {
	return lightLevelDependentMagicValue(m) < .5 && g.NearestAttackableTargetGoal.CanUse(m)
}

// infestedHost returns the infested block a silverfish turns a block into,
// or nil, as InfestedBlock.infestedStateByHost.
func infestedHost(b world.Block) world.Block {
	switch b := b.(type) {
	case block.Stone:
		if !b.Smooth {
			return block.InfestedStone{}
		}
	case block.Cobblestone:
		if !b.Mossy {
			return block.InfestedCobblestone{}
		}
	case block.StoneBricks:
		return block.InfestedStoneBricks{Type: b.Type}
	case block.Deepslate:
		return block.InfestedDeepslate{Axis: b.Axis}
	}
	return nil
}

// silverfishWakeUpFriendsGoal makes a hurt silverfish wake the silverfish
// in infested blocks around it, ported from Java's
// Silverfish.SilverfishWakeUpFriendsGoal.
type silverfishWakeUpFriendsGoal struct {
	goalFlags
	lookForFriends int
}

func (g *silverfishWakeUpFriendsGoal) notifyHurt() {
	if g.lookForFriends == 0 {
		g.lookForFriends = adjustedTickDelay(g, 20)
	}
}

func (g *silverfishWakeUpFriendsGoal) CanUse(*Mob) bool { return g.lookForFriends > 0 }

func (g *silverfishWakeUpFriendsGoal) Tick(m *Mob) {
	if g.lookForFriends--; g.lookForFriends > 0 {
		return
	}
	base := m.BlockPos()
	r := m.Random()
	next := func(off int) int {
		if off <= 0 {
			return 1 - off
		}
		return -off
	}
	for y := 0; y <= 5 && y >= -5; y = next(y) {
		for x := 0; x <= 10 && x >= -10; x = next(x) {
			for z := 0; z <= 10 && z >= -10; z = next(z) {
				pos := base.Add(cube.Pos{x, y, z})
				host, ok := infestedBlockHost(m.tx.Block(pos))
				if !ok {
					continue
				}
				if mobGriefingDisabled(m.tx) {
					m.tx.SetBlock(pos, host, nil)
				} else {
					breakBlockReleasingSilverfish(m.tx, pos)
				}
				if r.NextBoolean() {
					return
				}
			}
		}
	}
}

// infestedBlockHost returns the block an infested block hides in.
func infestedBlockHost(b world.Block) (world.Block, bool) {
	switch b := b.(type) {
	case block.InfestedStone:
		return block.Stone{}, true
	case block.InfestedCobblestone:
		return block.Cobblestone{}, true
	case block.InfestedStoneBricks:
		return block.StoneBricks{Type: b.Type}, true
	case block.InfestedDeepslate:
		return block.Deepslate{Axis: b.Axis}, true
	}
	return nil, false
}

// breakBlockReleasingSilverfish destroys an infested block, which releases
// its silverfish.
func breakBlockReleasingSilverfish(tx *world.Tx, pos cube.Pos) {
	b := tx.Block(pos)
	tx.SetBlock(pos, nil, nil)
	tx.AddParticle(pos.Vec3Centre(), particle.BlockBreak{Block: b})
	tx.AddEntity(NewSilverfish(world.EntitySpawnOpts{Position: pos.Vec3Middle()}))
}

// silverfishMergeWithStoneGoal makes an idle silverfish hide in stone,
// ported from Java's Silverfish.SilverfishMergeWithStoneGoal.
type silverfishMergeWithStoneGoal struct {
	*RandomStrollGoal
	dir     cube.Face
	doMerge bool
}

func newSilverfishMergeWithStoneGoal() *silverfishMergeWithStoneGoal {
	g := NewRandomStrollGoal(1)
	g.Interval = 10
	return &silverfishMergeWithStoneGoal{RandomStrollGoal: g}
}

func (g *silverfishMergeWithStoneGoal) mergePos(m *Mob) cube.Pos {
	return cube.PosFromVec3(mgl64.Vec3{m.data.Pos[0], m.data.Pos[1] + .5, m.data.Pos[2]}).Side(g.dir)
}

func (g *silverfishMergeWithStoneGoal) CanUse(m *Mob) bool {
	if m.Target() != nil || !m.AI.nav.IsDone() {
		return false
	}
	r := m.Random()
	if !mobGriefingDisabled(m.tx) && r.NextInt(int32(reducedTickDelay(10))) == 0 {
		g.dir = cube.Face(r.NextInt(6))
		if infestedHost(m.tx.Block(g.mergePos(m))) != nil {
			g.doMerge = true
			return true
		}
	}
	g.doMerge = false
	return g.RandomStrollGoal.CanUse(m)
}

func (g *silverfishMergeWithStoneGoal) CanContinueToUse(m *Mob) bool {
	return !g.doMerge && g.RandomStrollGoal.CanContinueToUse(m)
}

func (g *silverfishMergeWithStoneGoal) Start(m *Mob) {
	if !g.doMerge {
		g.RandomStrollGoal.Start(m)
		return
	}
	pos := g.mergePos(m)
	if infested := infestedHost(m.tx.Block(pos)); infested != nil {
		m.tx.SetBlock(pos, infested, nil)
		(&speciesMob{Ent: m.Ent, kind: silverfishSpecies}).view(SilverfishMergeAction{})
		_ = m.CloseIn(m.tx)
	}
}

// silverfishAlwaysTriggered reports if damage is in the
// always_triggers_silverfish damage type tag: magic.
func silverfishAlwaysTriggered(src world.DamageSource) bool {
	_, magic := src.(effect.InstantDamageSource)
	return magic
}
