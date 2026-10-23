package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// This file ports Java's Enderman: its goals, its anger as a NeutralMob, its
// teleports and the block it carries.

// endermanSpeedBonus is the movement speed an enderman gains while it has a
// target, Java's SPEED_MODIFIER_ATTACKING.
const endermanSpeedBonus = .15

func endermanState(m *Mob) *speciesState { return m.Behaviour().(*speciesState) }

// registerEndermanGoals ports Enderman.registerGoals.
func (s *speciesState) registerEndermanGoals(m *Mob) {
	ai := m.AI
	ai.SetPathfindingMalus(PathWater, -1)
	ai.Goals.Add(0, NewFloatGoal(m))
	ai.Goals.Add(1, &endermanFreezeWhenLookedAtGoal{goalFlags: goalFlags{GoalJump | GoalMove}})
	ai.Goals.Add(2, NewMeleeAttackGoal(1, false))
	stroll := NewWaterAvoidingRandomStrollGoal(1)
	stroll.Probability = 0
	ai.Goals.Add(7, stroll)
	ai.Goals.Add(8, NewLookAtPlayerGoal(8))
	ai.Goals.Add(8, NewRandomLookAroundGoal())
	ai.Goals.Add(10, &endermanLeaveBlockGoal{})
	ai.Goals.Add(11, &endermanTakeBlockGoal{})
	ai.Targets.Add(1, &endermanLookForPlayerGoal{NearestAttackableTargetGoal: NewNearestAttackableTargetGoal(nil, false)})
	ai.Targets.Add(2, NewHurtByTargetGoal())
	ai.Targets.Add(3, NewNearestAttackableTargetGoal(entityIs("minecraft:endermite"), true))
	ai.Targets.Add(4, &resetUniversalAngerTargetGoal{lastHurtByPlayer: -1})
}

// targetSet ports Enderman.setTarget: an enderman with a target screams and
// moves faster.
func (s *speciesState) targetSet(m *Mob, target world.Entity) {
	if s.kind != endermanSpecies {
		return
	}
	creepy := target != nil
	if creepy {
		s.targetChangeTime = m.AI.tickCount
		if !s.creepy && s.staredAt && m.AI.tickCount >= s.lastStareSound+400 {
			s.lastStareSound = m.AI.tickCount
			m.tx.PlaySound(mgl64.Vec3{m.data.Pos[0], m.EyeY(), m.data.Pos[2]}, sound.Custom{Name: "entity.enderman.stare", Volume: 2.5, Pitch: 1})
		}
	} else {
		s.targetChangeTime = 0
		s.staredAt = false
	}
	m.AI.MovementSpeed = s.speed
	if creepy {
		m.AI.MovementSpeed += endermanSpeedBonus
	}
	if creepy != s.creepy {
		s.creepy = creepy
		m.updateState()
	}
}

// customServerAiStep ports Enderman.customServerAiStep: an enderman in the
// sun without a target teleports away now and then.
func (s *speciesState) customServerAiStep(m *Mob) {
	if s.kind != endermanSpecies || !brightOutside(m.tx) || m.AI.tickCount < s.targetChangeTime+600 {
		return
	}
	br := lightLevelDependentMagicValue(m)
	if br > .5 && canSeeSky(m.tx, m.BlockPos()) && float64(m.Random().NextFloat())*30 < (br-.4)*2 {
		m.SetTarget(nil)
		endermanTeleport(m)
	}
}

// tickEndermanWater ports the damage water deals to an enderman in
// LivingEntity.aiStep, for isSensitiveToWater, while it stands in water or
// rain.
func (s *speciesState) tickEndermanWater(m *Mob) {
	top := mgl64.Vec3{m.data.Pos[0], m.BBox().Max()[1], m.data.Pos[2]}
	if m.InWater() || m.tx.RainingAt(m.BlockPos()) || m.tx.RainingAt(cube.PosFromVec3(top)) {
		if l, ok := m.Self.(Living); ok {
			l.Hurt(1, DrowningDamageSource{})
		}
	}
}

// endermanHurt ports the start of Enderman.hurtServer. It returns false if
// the enderman dodges the damage, and a function to run once the damage was
// dealt.
func (m *speciesMob) endermanHurt(src world.DamageSource) (func(), bool) {
	mob := m.s().ai.Mob(m.Ent, speciesSelf(m.Ent, endermanSpecies))
	switch src.(type) {
	case ProjectileDamageSource:
		if !isPassenger(mob.Self) {
			endermanTeleportRepeatedly(mob)
			return nil, false
		}
	case SplashPotionDamageSource:
		return func() { endermanTeleportRepeatedly(mob) }, true
	}
	if _, living := damageAttacker(src).(Living); !living {
		return func() {
			if mob.Random().NextInt(10) != 0 {
				endermanTeleport(mob)
			}
		}, true
	}
	return nil, true
}

// endermanTeleport ports Enderman.teleport(): to a random position within 32
// blocks.
func endermanTeleport(m *Mob) bool {
	if !m.Alive() {
		return false
	}
	r := m.Random()
	x := m.data.Pos[0] + (r.NextDouble()-.5)*64
	y := m.data.Pos[1] + float64(r.NextInt(64)-32)
	z := m.data.Pos[2] + (r.NextDouble()-.5)*64
	return endermanTeleportTo(m, x, y, z)
}

// endermanTeleportTowards ports Enderman.teleportTowards: to around 16
// blocks closer to an entity.
func endermanTeleportTowards(m *Mob, e world.Entity) bool {
	dir := mgl64.Vec3{m.data.Pos[0] - e.Position()[0], m.data.Pos[1] + m.Height()*.5 - entityEyeY(e), m.data.Pos[2] - e.Position()[2]}
	if dir.Len() < 1e-5 {
		dir = mgl64.Vec3{}
	} else {
		dir = dir.Normalize()
	}
	r := m.Random()
	x := m.data.Pos[0] + (r.NextDouble()-.5)*8 - dir[0]*16
	y := m.data.Pos[1] + float64(r.NextInt(16)-8) - dir[1]*16
	z := m.data.Pos[2] + (r.NextDouble()-.5)*8 - dir[2]*16
	return endermanTeleportTo(m, x, y, z)
}

// endermanTeleportRepeatedly ports Enderman.repeatedlyTryToTeleport.
func endermanTeleportRepeatedly(m *Mob) {
	if isPassenger(m.Self) {
		return
	}
	for range 64 {
		if endermanTeleport(m) {
			return
		}
	}
}

// endermanTeleportTo ports Enderman.teleport(x, y, z).
func endermanTeleportTo(m *Mob, x, y, z float64) bool {
	if isPassenger(m.Self) {
		return false
	}
	old := m.data.Pos
	invalid := func(b world.Block) bool { return blockInTag(b, "minecraft:enderman_does_not_teleport_to") }
	// Enderman.canRandomlyTeleportTo: never onto water.
	allowed := func(x, y, z float64) bool {
		l, ok := m.tx.Liquid(cube.PosFromVec3(mgl64.Vec3{x, y, z}).Side(cube.FaceDown))
		_, water := l.(block.Water)
		return !ok || !water
	}
	if !randomTeleport(m, x, y, z, invalid, allowed) {
		return false
	}
	m.tx.EmitGameEvent(world.GameEventTeleport, old, world.GameEventContext{Source: m.Self})
	m.tx.AddParticle(old, particle.EndermanTeleport{})
	m.tx.PlaySound(old, sound.Custom{Name: "entity.enderman.teleport", Volume: 1, Pitch: 1})
	m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "entity.enderman.teleport", Volume: 1, Pitch: 1})
	return true
}

// randomTeleport ports LivingEntity.randomTeleport: the mob drops from the
// position to the first block entities may teleport onto, and moves there if
// it fits without touching liquid or a block invalid selects and allowed
// accepts the position.
func randomTeleport(m *Mob, x, y, z float64, invalid func(world.Block) bool, allowed func(x, y, z float64) bool) bool {
	pos := cube.PosFromVec3(mgl64.Vec3{x, y, z})
	if !m.tx.ChunkLoaded(world.ChunkPos{int32(pos[0] >> 4), int32(pos[2] >> 4)}) {
		return false
	}
	for pos[1] > m.tx.Range().Min() {
		pos = pos.Side(cube.FaceDown)
		b := m.tx.Block(pos)
		if !blockInTag(b, "minecraft:entities_can_teleport_to") {
			y--
			continue
		}
		if invalid(b) {
			return false
		}
		dest := mgl64.Vec3{x, y, z}
		box := m.H().Type().BBox(m.Self).Translate(dest)
		if !boxFreeOfLiquid(m, box) || blockWithin(m.tx, box, invalid) || !allowed(x, y, z) {
			return false
		}
		m.Teleport(dest)
		m.AI.nav.Stop()
		return true
	}
	return false
}

// blockWithin reports if a block of a box matches f.
func blockWithin(tx *world.Tx, box cube.BBox, f func(world.Block) bool) bool {
	minX, minY, minZ := int(math.Floor(box.Min()[0])), int(math.Floor(box.Min()[1])), int(math.Floor(box.Min()[2]))
	maxX, maxY, maxZ := int(math.Floor(box.Max()[0])), int(math.Floor(box.Max()[1])), int(math.Floor(box.Max()[2]))
	for x := minX; x <= maxX; x++ {
		for y := minY; y <= maxY; y++ {
			for z := minZ; z <= maxZ; z++ {
				if f(tx.Block(cube.Pos{x, y, z})) {
					return true
				}
			}
		}
	}
	return false
}

// endermanStaredAt ports Enderman.isBeingStaredBy: a player without a
// carved pumpkin on looks the enderman in the eyes.
func endermanStaredAt(m *Mob, p world.Entity) bool {
	if !isPlayerEntity(p) || creakingDisguise(p) {
		return false
	}
	return lookingAtMe(m, p, .025, true, m.EyeY())
}

// lookingAtMe ports LivingEntity.isLookingAtMe with a collision clip: the
// viewer looks within a cone at one of the gaze heights of the mob, a cone
// narrower with distance if adjustForDistance, and sees it there.
func lookingAtMe(m *Mob, viewer world.Entity, cone float64, adjustForDistance bool, gazeHeights ...float64) bool {
	rot, ok := viewer.(interface{ Rotation() cube.Rotation })
	if !ok {
		return false
	}
	look := rot.Rotation().Vec3().Normalize()
	eye := mgl64.Vec3{viewer.Position()[0], entityEyeY(viewer), viewer.Position()[2]}
	for _, h := range gazeHeights {
		gaze := mgl64.Vec3{m.data.Pos[0], h, m.data.Pos[2]}
		dir := gaze.Sub(eye)
		dist := dir.Len()
		if dist < 1e-5 {
			continue
		}
		limit := cone
		if adjustForDistance {
			limit /= dist
		}
		if look.Dot(dir.Mul(1/dist)) > 1-limit && lineOfSight(m.tx, eye, gaze) {
			return true
		}
	}
	return false
}

// indirectPassenger reports if an entity rides the mob, directly or on
// something riding it, as Entity.hasIndirectPassenger.
func indirectPassenger(m *Mob, e world.Entity) bool {
	for range 16 {
		r, ok := e.(Rider)
		if !ok || r.Riding() == nil {
			return false
		}
		if r.Riding() == m.H() {
			return true
		}
		if e, ok = r.Riding().Entity(m.tx); !ok {
			return false
		}
	}
	return false
}

// endermanFreezeWhenLookedAtGoal stops an enderman while the player it
// targets stares at it, ported from Enderman.EndermanFreezeWhenLookedAt.
type endermanFreezeWhenLookedAtGoal struct {
	goalFlags
	target *world.EntityHandle
}

func (g *endermanFreezeWhenLookedAtGoal) CanUse(m *Mob) bool {
	target := m.Target()
	if target == nil || !isPlayerEntity(target) {
		return false
	}
	g.target = target.H()
	return m.DistanceToSqr(target) <= 256 && endermanStaredAt(m, target)
}

func (g *endermanFreezeWhenLookedAtGoal) Start(m *Mob) { m.AI.nav.Stop() }

func (g *endermanFreezeWhenLookedAtGoal) Tick(m *Mob) {
	if t, ok := g.target.Entity(m.tx); ok {
		m.AI.look.SetLookAt(m, t.Position()[0], entityEyeY(t), t.Position()[2])
	}
}

// endermanLookForPlayerGoal targets a player that stares at an enderman or
// angered it, and teleports the enderman around it, ported from
// Enderman.EndermanLookForPlayerGoal.
type endermanLookForPlayerGoal struct {
	*NearestAttackableTargetGoal
	pending                 *world.EntityHandle
	aggroTime, teleportTime int
}

func (g *endermanLookForPlayerGoal) angerInducing(m *Mob, target world.Entity) bool {
	return (endermanStaredAt(m, target) || endermanState(m).angryAt(m, target)) && !indirectPassenger(m, target)
}

func (g *endermanLookForPlayerGoal) CanUse(m *Mob) bool {
	c := forCombat()
	c.rangeLimit = m.AI.FollowRange
	c.selector = g.angerInducing
	g.pending = nil
	if p := nearestPlayer(m, c); p != nil {
		g.pending = p.H()
	}
	return g.pending != nil
}

func (g *endermanLookForPlayerGoal) Start(m *Mob) {
	g.aggroTime = adjustedTickDelay(g, 5)
	g.teleportTime = 0
	endermanState(m).staredAt = true
}

func (g *endermanLookForPlayerGoal) Stop(m *Mob) {
	g.pending = nil
	g.NearestAttackableTargetGoal.Stop(m)
}

func (g *endermanLookForPlayerGoal) CanContinueToUse(m *Mob) bool {
	if g.pending != nil {
		p, ok := g.pending.Entity(m.tx)
		if !ok || !g.angerInducing(m, p) {
			return false
		}
		m.LookAt(p, 10, 10)
		return true
	}
	if g.target != nil {
		if t, ok := g.target.Entity(m.tx); ok {
			if indirectPassenger(m, t) {
				return false
			}
			c := forCombat()
			c.ignoreSight = true
			if c.test(m, t) {
				return true
			}
		}
	}
	return g.NearestAttackableTargetGoal.CanContinueToUse(m)
}

func (g *endermanLookForPlayerGoal) Tick(m *Mob) {
	if m.Target() == nil {
		g.target = nil
	}
	if g.pending != nil {
		if g.aggroTime--; g.aggroTime <= 0 {
			g.target, g.pending = g.pending, nil
			g.NearestAttackableTargetGoal.Start(m)
		}
		return
	}
	if g.target == nil || isPassenger(m.Self) {
		return
	}
	t, ok := g.target.Entity(m.tx)
	if !ok {
		return
	}
	if endermanStaredAt(m, t) {
		if m.DistanceToSqr(t) < 16 {
			endermanTeleport(m)
		}
		g.teleportTime = 0
	} else if m.DistanceToSqr(t) > 256 {
		waited := g.teleportTime
		g.teleportTime++
		if waited >= adjustedTickDelay(g, 30) && endermanTeleportTowards(m, t) {
			g.teleportTime = 0
		}
	}
}

// neutralMob is implemented by the behaviours of mobs angered as Java's
// NeutralMob.
type neutralMob interface {
	forgetCurrentTargetAndRefreshUniversalAnger(m *Mob)
}

// resetUniversalAngerTargetGoal makes a mob hit by a player angry at all
// players under the universal_anger game rule, ported from Java's
// ResetUniversalAngerTargetGoal without alerting others.
type resetUniversalAngerTargetGoal struct {
	goalFlags
	lastHurtByPlayer int
}

func (g *resetUniversalAngerTargetGoal) CanUse(m *Mob) bool {
	if !m.tx.World().GameRuleBool("universal_anger") {
		return false
	}
	attacker, timestamp := m.LastHurtBy()
	return attacker != nil && isPlayerEntity(attacker) && timestamp > g.lastHurtByPlayer
}

func (g *resetUniversalAngerTargetGoal) Start(m *Mob) {
	_, g.lastHurtByPlayer = m.LastHurtBy()
	if n, ok := m.Behaviour().(neutralMob); ok {
		n.forgetCurrentTargetAndRefreshUniversalAnger(m)
	}
}

// angry ports NeutralMob.isAngry.
func (s *speciesState) angry() bool { return s.angerTicks > 0 }

// startAngerTimer ports Enderman.startPersistentAngerTimer: 20 to 39
// seconds.
func (s *speciesState) startAngerTimer(m *Mob) { s.angerTicks = 400 + int(m.Random().NextInt(381)) }

// stopBeingAngry ports NeutralMob.stopBeingAngry.
func (s *speciesState) stopBeingAngry(m *Mob) {
	m.AI.SetLastHurtBy(nil)
	s.angerTarget = uuid.Nil
	m.SetTarget(nil)
	s.angerTicks = 0
}

func (s *speciesState) forgetCurrentTargetAndRefreshUniversalAnger(m *Mob) {
	s.stopBeingAngry(m)
	s.startAngerTimer(m)
}

// angryAt ports NeutralMob.isAngryAt.
func (s *speciesState) angryAt(m *Mob, e world.Entity) bool {
	if !m.CanAttack(e) {
		return false
	}
	if validPlayerTarget(m.tx, e) && m.tx.World().GameRuleBool("universal_anger") && s.angry() && s.angerTarget == uuid.Nil {
		return true
	}
	return s.angerTarget != uuid.Nil && s.angerTarget == e.H().UUID()
}

// updatePersistentAnger ports NeutralMob.updatePersistentAnger for an
// enderman, which stays angry while it has a target.
func (s *speciesState) updatePersistentAnger(m *Mob) {
	if s.angerTicks > 0 {
		s.angerTicks--
	}
	angerTarget := s.angerTarget
	var previous world.Entity
	if m.AI.target != nil {
		previous, _ = m.AI.target.Entity(m.tx)
	}
	if previous != nil && !entityAlive(previous) && angerTarget != uuid.Nil && angerTarget == previous.H().UUID() && !isPlayerEntity(previous) {
		s.stopBeingAngry(m)
		return
	}
	target := m.Target()
	if target != nil {
		s.angerTarget = target.H().UUID()
		s.startAngerTimer(m)
	}
	if angerTarget != uuid.Nil && !s.angry() && (target == nil || !validPlayerTarget(m.tx, target)) {
		s.stopBeingAngry(m)
	}
	if angerTarget == uuid.Nil {
		return
	}
	if p, ok := m.tx.EntityByUUID(angerTarget); ok && isPlayerEntity(p) && (creativeOrSpectator(p) || m.tx.World().Difficulty() == world.DifficultyPeaceful) {
		s.stopBeingAngry(m)
	}
}

// setCarried sets the block an enderman carries, nil for none.
func (s *speciesState) setCarried(m *Mob, b world.Block) {
	s.carried = b
	m.updateState()
}

// endermanTakeBlockGoal makes an enderman pick up a block next to it,
// ported from Enderman.EndermanTakeBlockGoal.
type endermanTakeBlockGoal struct{ goalFlags }

func (g *endermanTakeBlockGoal) CanUse(m *Mob) bool {
	return endermanState(m).carried == nil && !mobGriefingDisabled(m.tx) && m.Random().NextInt(int32(reducedTickDelay(20))) == 0
}

func (g *endermanTakeBlockGoal) Tick(m *Mob) {
	r := m.Random()
	p := m.data.Pos
	xt := int(math.Floor(p[0] - 2 + r.NextDouble()*4))
	yt := int(math.Floor(p[1] + r.NextDouble()*3))
	zt := int(math.Floor(p[2] - 2 + r.NextDouble()*4))
	pos := cube.Pos{xt, yt, zt}
	b := m.tx.Block(pos)
	feet := m.BlockPos()
	from := mgl64.Vec3{float64(feet[0]) + .5, float64(yt) + .5, float64(feet[2]) + .5}
	if !blockInTag(b, "minecraft:enderman_holdable") || !outlineClipReaches(m.tx, from, pos.Vec3Centre(), pos) {
		return
	}
	m.tx.SetBlock(pos, nil, nil)
	m.tx.EmitGameEvent(world.GameEventBlockDestroy, pos.Vec3Centre(), world.GameEventContext{Source: m.Self, AffectedState: b})
	endermanState(m).setCarried(m, defaultBlockState(b))
}

// outlineClipReaches reports if a clip of block outlines from one point to
// another first hits the block at pos, or nothing.
func outlineClipReaches(tx *world.Tx, from, to mgl64.Vec3, pos cube.Pos) bool {
	reaches := true
	trace.TraverseBlocks(from, to, func(at cube.Pos) bool {
		if _, hit := trace.BlockIntercept(at, tx, pathBlockAt(tx, at), from, to, trace.RayShape); hit {
			reaches = at == pos
			return false
		}
		return true
	})
	return reaches
}

// defaultBlockState returns a block in the default state of its Java block,
// as Block.defaultBlockState.
func defaultBlockState(b world.Block) world.Block {
	state, err := world.CanonicalStateOf(b)
	if err != nil {
		return b
	}
	id, err := gamedata.BlockStateID(state.Name, gamedata.BlockPropertiesWithDefaults(state.Name, nil))
	if err != nil {
		return b
	}
	if d, ok := world.BlockByRuntimeID(world.ResolveStateID(world.DefaultBlockRegistry, id).Block); ok {
		return d
	}
	return b
}

// endermanLeaveBlockGoal makes an enderman put down the block it carries,
// ported from Enderman.EndermanLeaveBlockGoal.
type endermanLeaveBlockGoal struct{ goalFlags }

func (g *endermanLeaveBlockGoal) CanUse(m *Mob) bool {
	return endermanState(m).carried != nil && !mobGriefingDisabled(m.tx) && m.Random().NextInt(int32(reducedTickDelay(2000))) == 0
}

func (g *endermanLeaveBlockGoal) Tick(m *Mob) {
	r := m.Random()
	p := m.data.Pos
	pos := cube.Pos{int(math.Floor(p[0] - 1 + r.NextDouble()*2)), int(math.Floor(p[1] + r.NextDouble()*2)), int(math.Floor(p[2] - 1 + r.NextDouble()*2))}
	s := endermanState(m)
	if s.carried == nil {
		return
	}
	if _, air := m.tx.Block(pos).(block.Air); !air {
		return
	}
	below := pos.Side(cube.FaceDown)
	belowBlock := m.tx.Block(below)
	switch belowBlock.(type) {
	case block.Air, block.Bedrock:
		return
	}
	if !fullCollisionCube(m.tx, below, belowBlock) {
		return
	}
	state, err := world.CanonicalStateOf(s.carried)
	if err != nil {
		return
	}
	id, err := gamedata.BlockStateID(state.Name, state.Properties)
	if err != nil {
		return
	}
	placed, survives := m.tx.JavaPlacement(id, pos)
	if !survives {
		return
	}
	for e := range m.tx.EntitiesWithin(cube.Box(0, 0, 0, 1, 1, 1).Translate(pos.Vec3())) {
		if e.H() != m.H() {
			return
		}
	}
	m.tx.SetJavaBlockState(pos, placed)
	m.tx.EmitGameEvent(world.GameEventBlockPlace, pos.Vec3Centre(), world.GameEventContext{Source: m.Self, AffectedState: s.carried})
	s.setCarried(m, nil)
}

// dropCustomDeathLoot ports Enderman.dropCustomDeathLoot: the block an
// enderman carries drops as if mined with a silk touch diamond axe.
func (m *speciesMob) dropCustomDeathLoot(bool, int) {
	s := m.s()
	if s.kind != endermanSpecies || s.carried == nil {
		return
	}
	breakable, ok := s.carried.(block.Breakable)
	if !ok || breakable.BreakInfo().Drops == nil {
		return
	}
	silkTouch := []item.Enchantment{item.NewEnchantment(enchantment.SilkTouch, 1)}
	for _, stack := range breakable.BreakInfo().Drops(item.Axe{Tier: item.ToolTierDiamond}, silkTouch) {
		m.tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: m.Position()}, stack))
	}
}
