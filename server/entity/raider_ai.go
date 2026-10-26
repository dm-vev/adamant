package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// This file ports Java's Raider, PatrollingMonster and AbstractIllager: the
// goals raiders share in patrols and raids.

// raiderState is the state Java's PatrollingMonster and Raider keep: the
// patrol of the raider and whether it celebrates a won raid.
type raiderState struct {
	patrolLeader, patrolling, hasPatrolTarget bool
	patrolTarget                              cube.Pos
	celebrating                               bool
}

func (r *raiderState) raider() *raiderState { return r }

// raiderOf returns the raider state of a mob.
func raiderOf(m *Mob) *raiderState {
	return m.Behaviour().(interface{ raider() *raiderState }).raider()
}

// isRaiderEntity reports if an entity is one of Java's Raider.
func isRaiderEntity(e world.Entity) bool {
	return hostileRaidMob(e.H().Type().EncodeEntity()) || e.H().Type().EncodeEntity() == "minecraft:witch"
}

// hostileIllager reports if a kind is one of Java's AbstractIllager.
func hostileIllager(kind string) bool {
	switch kind {
	case "minecraft:evoker", "minecraft:vindicator", "minecraft:pillager":
		return true
	}
	return false
}

// raid returns the raid the raider takes part in, if any.
func (r *raiderState) raid(m *Mob) *world.Raid { return m.tx.RaidForRaider(m.Self) }

// activeRaid ports Raider.hasActiveRaid with the raid not over.
func (r *raiderState) activeRaid(m *Mob) *world.Raid {
	if raid := r.raid(m); raid != nil && raid.Active() {
		return raid
	}
	return nil
}

// alliedTo ports AbstractIllager.considersEntityAsAlly: raiders and the
// illager_friends stick together.
func (b *hostileState) alliedTo(m *Mob, other world.Entity) bool {
	if !hostileRaidMob(b.kind) {
		return false
	}
	if isRaiderEntity(other) || entityTypeInTag(other, "minecraft:illager_friends") {
		return true
	}
	if b.kind == "minecraft:evoker" {
		if vex, ok := other.(*Vex); ok {
			if owner := vex.state().vexOwner; owner != nil && owner == m.H() {
				return true
			}
		}
	}
	return false
}

// canAttackEntity ports AbstractIllager.canAttack: illagers spare baby
// villagers.
func (b *hostileState) canAttackEntity(m *Mob, target world.Entity) bool {
	if hostileIllager(b.kind) && isVillagerEntity(target) && mobIsBaby(target) {
		return false
	}
	return true
}

func isVillagerEntity(e world.Entity) bool {
	switch e.H().Type().EncodeEntity() {
	case "minecraft:villager", "minecraft:villager_v2", "minecraft:wandering_trader":
		return true
	}
	return false
}

// raiderTick runs what Raider.aiStep does before the AI of a raider: one
// fighting a player or iron golem in a raid never idles.
func (b *hostileState) raiderTick(m *Mob) {
	// Raider.updateNoActionTime counts twice as fast.
	m.AI.noActionTime++
	if b.activeRaid(m) == nil {
		return
	}
	if t := m.Target(); t != nil {
		switch t.H().Type().EncodeEntity() {
		case "minecraft:player", "minecraft:iron_golem":
			m.AI.noActionTime = 0
		}
	}
}

// registerRaiderGoals ports PatrollingMonster.registerGoals and
// Raider.registerGoals, which every raider runs before its own.
func registerRaiderGoals(m *Mob) {
	ai := m.AI
	ai.Goals.Add(1, &obtainRaidLeaderBannerGoal{goalFlags: goalFlags{GoalMove}})
	ai.Goals.Add(4, &raiderPatrolGoal{goalFlags: goalFlags{GoalMove}})
	ai.Goals.Add(3, &pathfindToRaidGoal{goalFlags: goalFlags{GoalMove}})
	ai.Goals.Add(4, &raiderMoveThroughVillageGoal{goalFlags: goalFlags{GoalMove}})
	ai.Goals.Add(5, &raiderCelebrationGoal{goalFlags{GoalMove}})
}

// raiderHurtByTarget returns Java's HurtByTargetGoal(this, Raider.class)
// that alerts others: raiders ignore damage from other raiders.
func raiderHurtByTarget() *HurtByTargetGoal {
	g := NewHurtByTargetGoal()
	g.IgnoreDamageFrom = isRaiderEntity
	g.AlertOthers = true
	return g
}

// raiderPatrolGoal ports PatrollingMonster.LongDistancePatrolGoal: a
// patrolling raider walks towards its patrol target in legs of ten blocks
// with its companions, the leader picking new far targets.
type raiderPatrolGoal struct {
	goalFlags
	cooldownUntil int64
}

func (g *raiderPatrolGoal) CanUse(m *Mob) bool {
	b := raiderOf(m)
	return b.patrolling && m.Target() == nil && !isVehicle(m.Self) && b.hasPatrolTarget && m.tx.CurrentTick() >= g.cooldownUntil
}

func (g *raiderPatrolGoal) Tick(m *Mob) {
	b := raiderOf(m)
	if !m.AI.nav.IsDone() {
		return
	}
	companions := raiderPatrolCompanions(m)
	switch {
	case len(companions) == 0:
		b.patrolling = false
	case b.patrolLeader && b.patrolTarget.Vec3Centre().Sub(m.data.Pos).LenSqr() < 100:
		b.findPatrolTarget(m.BlockPos(), m.Random())
	default:
		target := b.patrolTarget.Vec3Middle()
		d := m.data.Pos.Sub(target)
		// distance.yRot(90.0F).scale(0.4).add(target): Vec3.yRot takes
		// radians, so the distance turns by 90 radians.
		cos, sin := float64(float32(math.Cos(90))), float64(float32(math.Sin(90)))
		target = target.Add(mgl64.Vec3{d[0]*cos + d[2]*sin, 0, d[2]*cos - d[0]*sin}.Mul(.4))
		dir := target.Sub(m.data.Pos)
		if dir.Len() > 0 {
			dir = dir.Normalize()
		}
		leg := cube.PosFromVec3(m.data.Pos.Add(dir.Mul(10)))
		leg[1] = m.tx.JavaMotionBlockingHeight(leg[0], leg[2], true)
		speed := .7
		if b.patrolLeader {
			speed = .595
		}
		if !m.AI.nav.MoveToPos(m, float64(leg[0]), float64(leg[1]), float64(leg[2]), speed) {
			r := m.Random()
			random := m.BlockPos().Add(cube.Pos{-8 + int(r.NextInt(16)), 0, -8 + int(r.NextInt(16))})
			random[1] = m.tx.JavaMotionBlockingHeight(random[0], random[2], true)
			m.AI.nav.MoveToPos(m, float64(random[0]), float64(random[1]), float64(random[2]), .7)
			g.cooldownUntil = m.tx.CurrentTick() + 200
		} else if b.patrolLeader {
			for _, c := range companions {
				c.patrolTarget, c.hasPatrolTarget, c.patrolling = leg, true, true
			}
		}
	}
}

// findPatrolTarget ports PatrollingMonster.findPatrolTarget: a target up to
// 500 blocks away on each horizontal axis.
func (b *raiderState) findPatrolTarget(pos cube.Pos, r *vanilla.Random) {
	b.patrolTarget = pos.Add(cube.Pos{-500 + int(r.NextInt(1000)), 0, -500 + int(r.NextInt(1000))})
	b.hasPatrolTarget, b.patrolling = true, true
}

// raiderPatrolCompanions ports LongDistancePatrolGoal.findPatrolCompanions:
// the raiders within 16 blocks that may join a patrol.
func raiderPatrolCompanions(m *Mob) []*raiderState {
	var out []*raiderState
	for e := range m.tx.EntitiesWithin(m.BBox().Grow(16)) {
		if e.H() == m.H() || !isRaiderEntity(e) || !entityAlive(e) || m.tx.RaidForRaider(e) != nil {
			continue
		}
		if b, ok := e.(interface{ Behaviour() Behaviour }); ok {
			if r, ok := b.Behaviour().(interface{ raider() *raiderState }); ok {
				out = append(out, r.raider())
			}
		}
	}
	return out
}

// pathfindToRaidGoal ports Java's PathfindToRaidGoal: a raider in a raid
// heads for the village and recruits the raiders it passes.
type pathfindToRaidGoal struct {
	goalFlags
	recruitmentTick int
}

func (g *pathfindToRaidGoal) CanUse(m *Mob) bool {
	return m.Target() == nil && !isVehicle(m.Self) && g.CanContinueToUse(m)
}

func (g *pathfindToRaidGoal) CanContinueToUse(m *Mob) bool {
	return raiderOf(m).activeRaid(m) != nil && !m.tx.IsVillage(m.BlockPos())
}

func (g *pathfindToRaidGoal) Tick(m *Mob) {
	raid := raiderOf(m).activeRaid(m)
	if raid == nil {
		return
	}
	if m.AI.tickCount > g.recruitmentTick {
		g.recruitmentTick = m.AI.tickCount + 20
		for e := range m.tx.EntitiesWithin(m.BBox().Grow(16)) {
			if isRaiderEntity(e) && e.H() != m.H() && m.tx.RaidForRaider(e) == nil {
				m.tx.RegisterRaider(raid.ID(), e)
			}
		}
	}
	if m.AI.nav.IsDone() {
		center := raid.Center()
		if pos, ok := DefaultRandomPosTowards(m, 15, 4, mgl64.Vec3{float64(center[0]) + .5, float64(center[1]), float64(center[2]) + .5}, math.Pi/2); ok {
			m.AI.nav.MoveToPos(m, pos[0], pos[1], pos[2], 1)
		}
	}
}

// raiderMoveThroughVillageGoal ports Raider.RaiderMoveThroughVillageGoal: a
// raider walks from home to home through the village it raids.
type raiderMoveThroughVillageGoal struct {
	goalFlags
	poi     cube.Pos
	visited []cube.Pos
	stuck   bool
}

func (g *raiderMoveThroughVillageGoal) CanUse(m *Mob) bool {
	if len(g.visited) > 2 {
		g.visited = g.visited[1:]
	}
	return raiderOf(m).activeRaid(m) != nil && g.findHome(m) && m.Target() == nil
}

// findHome ports hasSuitablePoi: a random bed within 48 blocks the raider
// did not visit lately.
func (g *raiderMoveThroughVillageGoal) findHome(m *Mob) bool {
	origin := m.BlockPos()
	var homes []cube.Pos
	for pos, poi := range m.tx.POIsInSquare(origin, 48, "minecraft:bed") {
		if poi.Kind != world.Home || g.wasVisited(pos) {
			continue
		}
		if d := pos.Sub(origin); d.X()*d.X()+d.Y()*d.Y()+d.Z()*d.Z() <= 48*48 {
			homes = append(homes, pos)
		}
	}
	if len(homes) == 0 {
		return false
	}
	g.poi = homes[m.Random().NextInt(int32(len(homes)))]
	return true
}

func (g *raiderMoveThroughVillageGoal) wasVisited(pos cube.Pos) bool {
	for _, v := range g.visited {
		if v == pos {
			return true
		}
	}
	return false
}

func (g *raiderMoveThroughVillageGoal) CanContinueToUse(m *Mob) bool {
	reach := m.Width() + 1
	return !m.AI.nav.IsDone() && m.Target() == nil && g.poi.Vec3Centre().Sub(m.data.Pos).LenSqr() >= reach*reach && !g.stuck
}

func (g *raiderMoveThroughVillageGoal) Stop(m *Mob) {
	if g.poi.Vec3Centre().Sub(m.data.Pos).LenSqr() < 1 {
		g.visited = append(g.visited, g.poi)
	}
}

func (g *raiderMoveThroughVillageGoal) Start(m *Mob) {
	m.AI.noActionTime = 0
	m.AI.nav.MoveToPos(m, float64(g.poi[0]), float64(g.poi[1]), float64(g.poi[2]), 1.05)
	g.stuck = false
}

func (g *raiderMoveThroughVillageGoal) Tick(m *Mob) {
	if !m.AI.nav.IsDone() {
		return
	}
	poi := mgl64.Vec3{float64(g.poi[0]) + .5, float64(g.poi[1]), float64(g.poi[2]) + .5}
	pos, ok := DefaultRandomPosTowards(m, 16, 7, poi, math.Pi/10)
	if !ok {
		pos, ok = DefaultRandomPosTowards(m, 8, 7, poi, math.Pi/2)
	}
	if !ok {
		g.stuck = true
		return
	}
	m.AI.nav.MoveToPos(m, pos[0], pos[1], pos[2], 1.05)
}

// raiderCelebrationGoal ports Raider.RaiderCelebration: raiders that won a
// raid cheer and jump.
type raiderCelebrationGoal struct{ goalFlags }

func (g *raiderCelebrationGoal) CanUse(m *Mob) bool {
	raid := raiderOf(m).raid(m)
	return m.Alive() && m.Target() == nil && raid != nil && raid.Status() == world.RaidDefeat
}

func (g *raiderCelebrationGoal) Start(m *Mob) { raiderOf(m).setCelebrating(m, true) }

func (g *raiderCelebrationGoal) Stop(m *Mob) { raiderOf(m).setCelebrating(m, false) }

func (g *raiderCelebrationGoal) Tick(m *Mob) {
	r := m.Random()
	if r.NextInt(int32(adjustedTickDelay(g, 100))) == 0 {
		m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "entity." + m.H().Type().EncodeEntity()[len("minecraft:"):] + ".celebrate", Volume: 1, Pitch: 1})
	}
	if !isPassenger(m.Self) && r.NextInt(int32(adjustedTickDelay(g, 50))) == 0 {
		m.AI.jump.Jump()
	}
}

func (b *raiderState) setCelebrating(m *Mob, celebrating bool) {
	if b.celebrating != celebrating {
		b.celebrating = celebrating
		m.updateState()
	}
}

// holdGroundAttackGoal ports Raider.HoldGroundAttackGoal: a patrolling
// raider spotted by a target holds its ground until the target comes close,
// then the whole patrol attacks.
type holdGroundAttackGoal struct {
	goalFlags
	hostileRadiusSqr float64
}

func (g *holdGroundAttackGoal) CanUse(m *Mob) bool {
	b := raiderOf(m)
	if b.raid(m) != nil || !b.patrolling || m.Target() == nil || m.Aggressive() {
		return false
	}
	attacker, _ := m.LastHurtBy()
	return attacker == nil || !isPlayerEntity(attacker)
}

// nearbyRaiders returns the raiders within eight blocks of the mob.
func nearbyRaiders(m *Mob) []*Mob {
	var out []*Mob
	for e := range m.tx.EntitiesWithin(m.BBox().Grow(8)) {
		if e.H() == m.H() || !isRaiderEntity(e) || !entityAlive(e) || spectator(e) {
			continue
		}
		if other, ok := mobOf(e); ok {
			out = append(out, other)
		}
	}
	return out
}

func (g *holdGroundAttackGoal) Start(m *Mob) {
	m.AI.nav.Stop()
	for _, other := range nearbyRaiders(m) {
		other.SetTarget(m.Target())
	}
}

func (g *holdGroundAttackGoal) Stop(m *Mob) {
	target := m.Target()
	if target == nil {
		return
	}
	for _, other := range nearbyRaiders(m) {
		other.SetTarget(target)
		other.SetAggressive(true)
	}
	m.SetAggressive(true)
}

func (g *holdGroundAttackGoal) RequiresUpdateEveryTick() bool { return true }

func (g *holdGroundAttackGoal) Tick(m *Mob) {
	target := m.Target()
	if target == nil {
		return
	}
	if m.DistanceToSqr(target) > g.hostileRadiusSqr {
		m.AI.look.SetLookAtEntityWith(target, 30, 30)
	} else {
		m.SetAggressive(true)
	}
}

// openDoorGoal ports Java's OpenDoorGoal without closing the door after:
// a mob opens the wooden door it walks into.
type openDoorGoal struct {
	goalFlags
	door doorInteract
	// raidOnly limits the goal to raids, as AbstractIllager.RaiderOpenDoorGoal.
	raidOnly bool
}

func (g *openDoorGoal) CanUse(m *Mob) bool {
	if g.raidOnly && raiderOf(m).activeRaid(m) == nil {
		return false
	}
	return g.door.find(m)
}

func (g *openDoorGoal) CanContinueToUse(*Mob) bool { return false }

func (g *openDoorGoal) Start(m *Mob) {
	g.door.start(m)
	setDoorOpen(m.tx, g.door.pos, true)
}

func (g *openDoorGoal) Tick(m *Mob) { g.door.tick(m) }

// pillager returns the hostile state of a pillager holding a crossbow.
func (b *hostileState) holdingCrossbow() bool {
	_, ok := b.main.Item().(item.Crossbow)
	return ok
}

// registerPillagerGoals ports Pillager.registerGoals.
func registerPillagerGoals(m *Mob, b *hostileState) {
	ai := m.AI
	registerRaiderGoals(m)
	ai.Goals.Add(0, NewFloatGoal(m))
	ai.Goals.Add(1, NewAvoidEntityTypesGoal(entityTypes{"minecraft:creaking"}, 8, 1, 1.2))
	ai.Goals.Add(2, &holdGroundAttackGoal{goalFlags: goalFlags{GoalMove | GoalLook}, hostileRadiusSqr: 100})
	ai.Goals.Add(3, &rangedCrossbowAttackGoal{goalFlags: goalFlags{GoalMove | GoalLook}, speedModifier: 1, attackRadiusSqr: 64})
	ai.Goals.Add(8, NewRandomStrollGoal(.6))
	lookPlayer := NewLookAtPlayerGoal(15)
	lookPlayer.Probability = 1
	ai.Goals.Add(9, lookPlayer)
	lookMob := NewLookAtPlayerGoal(15)
	lookMob.IsType = isMobEntity
	ai.Goals.Add(10, lookMob)
	ai.Targets.Add(1, raiderHurtByTarget())
	ai.Targets.Add(2, NewNearestAttackableTargetGoal(nil, true))
	ai.Targets.Add(3, NewNearestAttackableTypesGoal(villagerTypes, false))
	ai.Targets.Add(3, NewNearestAttackableTypesGoal(entityTypes{"minecraft:iron_golem"}, true))
}

// isMobEntity reports if an entity is a mob, as Java's Mob class: a living
// entity other than a player or armour stand.
func isMobEntity(e world.Entity) bool {
	_, living := e.(Living)
	switch e.H().Type().EncodeEntity() {
	case "minecraft:player", "minecraft:armor_stand":
		return false
	}
	return living
}

// crossbowState is the state of RangedCrossbowAttackGoal.CrossbowState.
type crossbowState uint8

const (
	crossbowUncharged crossbowState = iota
	crossbowCharging
	crossbowCharged
	crossbowReady
)

// rangedCrossbowAttackGoal ports Java's RangedCrossbowAttackGoal: a pillager
// walks within eight blocks of its target, loads its crossbow, waits a
// second or two and shoots.
type rangedCrossbowAttackGoal struct {
	goalFlags
	speedModifier   float64
	attackRadiusSqr float64

	state          crossbowState
	seeTime        int
	attackDelay    int
	updatePathWait int
	chargeTicks    int
}

func (g *rangedCrossbowAttackGoal) validTarget(m *Mob) bool {
	t := m.Target()
	return t != nil && entityAlive(t)
}

func (g *rangedCrossbowAttackGoal) CanUse(m *Mob) bool {
	return g.validTarget(m) && hostileOf(m).holdingCrossbow()
}

func (g *rangedCrossbowAttackGoal) CanContinueToUse(m *Mob) bool {
	return g.validTarget(m) && (g.CanUse(m) || !m.AI.nav.IsDone()) && hostileOf(m).holdingCrossbow()
}

func (g *rangedCrossbowAttackGoal) Stop(m *Mob) {
	m.SetAggressive(false)
	m.SetTarget(nil)
	g.seeTime = 0
	b := hostileOf(m)
	if b.chargingCrossbow || b.crossbowCharged {
		b.setCrossbow(m, false, false)
	}
	g.state = crossbowUncharged
}

func (g *rangedCrossbowAttackGoal) RequiresUpdateEveryTick() bool { return true }

// crossbowChargeDuration ports CrossbowItem.getChargeDuration: a second and
// a quarter, a quarter second less per level of Quick Charge.
func crossbowChargeDuration(stack item.Stack) int {
	ticks := 25
	if e, ok := stack.Enchantment(enchantment.QuickCharge); ok {
		ticks -= 5 * e.Level()
	}
	return max(ticks, 0)
}

func (g *rangedCrossbowAttackGoal) Tick(m *Mob) {
	target := m.Target()
	if target == nil {
		return
	}
	b := hostileOf(m)
	sight := m.HasLineOfSight(target)
	if sight != (g.seeTime > 0) {
		g.seeTime = 0
	}
	if sight {
		g.seeTime++
	} else {
		g.seeTime--
	}
	dist := m.DistanceToSqr(target)
	needsToMove := (dist > g.attackRadiusSqr || g.seeTime < 5) && g.attackDelay == 0
	if needsToMove {
		if g.updatePathWait--; g.updatePathWait <= 0 {
			speed := g.speedModifier
			if g.state != crossbowUncharged {
				speed *= .5
			}
			m.AI.nav.MoveToEntity(m, target, speed)
			// PATHFINDING_DELAY_RANGE: one to two seconds.
			g.updatePathWait = 20 + int(m.Random().NextInt(21))
		}
	} else {
		g.updatePathWait = 0
		m.AI.nav.Stop()
	}
	m.AI.look.SetLookAtEntityWith(target, 30, 30)
	switch g.state {
	case crossbowUncharged:
		if !needsToMove {
			g.state, g.chargeTicks = crossbowCharging, 0
			b.setCrossbow(m, true, false)
		}
	case crossbowCharging:
		g.chargeTicks++
		duration := crossbowChargeDuration(b.main)
		switch {
		case g.chargeTicks == duration/5+1:
			m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "item.crossbow.loading_start", Volume: .5, Pitch: 1})
		case g.chargeTicks == duration/2+1:
			m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "item.crossbow.loading_middle", Volume: .5, Pitch: 1})
		}
		if g.chargeTicks >= duration {
			r := m.Random()
			m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "item.crossbow.loading_end", Volume: 1, Pitch: 1/(float64(r.NextFloat())*.5+1) + .2})
			g.state = crossbowCharged
			g.attackDelay = 20 + int(r.NextInt(20))
			b.setCrossbow(m, false, true)
		}
	case crossbowCharged:
		if g.attackDelay--; g.attackDelay == 0 {
			g.state = crossbowReady
		}
	case crossbowReady:
		if sight {
			b.performCrossbowAttack(m, target)
			b.setCrossbow(m, false, false)
			g.state = crossbowUncharged
		}
	}
}

// setCrossbow sets whether the pillager loads its crossbow and whether it
// holds it loaded.
func (b *hostileState) setCrossbow(m *Mob, charging, charged bool) {
	if b.chargingCrossbow != charging || b.crossbowCharged != charged {
		b.chargingCrossbow, b.crossbowCharged = charging, charged
		m.updateState()
	}
}

// performCrossbowAttack ports CrossbowAttackMob.performCrossbowAttack for a
// pillager: an arrow aimed a little above the target, at a power of 1.6.
func (b *hostileState) performCrossbowAttack(m *Mob, target world.Entity) {
	pos := mgl64.Vec3{m.data.Pos[0], m.EyeY() - .1, m.data.Pos[2]}
	tp := target.Position()
	xd, zd := tp[0]-m.data.Pos[0], tp[2]-m.data.Pos[2]
	yd := tp[1] + target.H().Type().BBox(target).Height()/3 - pos[1] + math.Hypot(xd, zd)*.2
	vel := shootDirection(m, mgl64.Vec3{xd, yd, zd}, 1.6, rangedAttackUncertainty(m))
	m.tx.AddEntity(NewArrowWithDamage(world.EntitySpawnOpts{Position: pos, Velocity: vel}, 2, m.Self))
	m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "item.crossbow.shoot", Volume: 1, Pitch: 1})
	m.AI.noActionTime = 0
}

// registerVindicatorGoals ports Vindicator.registerGoals.
func registerVindicatorGoals(m *Mob, b *hostileState) {
	ai := m.AI
	registerRaiderGoals(m)
	ai.Goals.Add(0, NewFloatGoal(m))
	ai.Goals.Add(1, NewAvoidEntityTypesGoal(entityTypes{"minecraft:creaking"}, 8, 1, 1.2))
	breakDoor := NewBreakDoorGoal(func(d world.Difficulty) bool {
		return d == world.DifficultyNormal || d == world.DifficultyHard
	})
	ai.Goals.Add(2, &vindicatorBreakDoorGoal{BreakDoorGoal: breakDoor})
	ai.Goals.Add(3, &openDoorGoal{raidOnly: true})
	ai.Goals.Add(4, &holdGroundAttackGoal{goalFlags: goalFlags{GoalMove | GoalLook}, hostileRadiusSqr: 100})
	ai.Goals.Add(5, NewMeleeAttackGoal(1, false))
	ai.Targets.Add(1, raiderHurtByTarget())
	ai.Targets.Add(2, NewNearestAttackableTargetGoal(nil, true))
	ai.Targets.Add(3, NewNearestAttackableTypesGoal(villagerTypes, true))
	ai.Targets.Add(3, NewNearestAttackableTypesGoal(entityTypes{"minecraft:iron_golem"}, true))
	johnny := NewNearestAttackableTargetGoal(func(e world.Entity) bool { _, ok := e.(Living); return ok }, true)
	johnny.RandomInterval = 0
	johnny.mustReach = true
	ai.Targets.Add(4, &vindicatorJohnnyAttackGoal{NearestAttackableTargetGoal: johnny})
	ai.Goals.Add(8, NewRandomStrollGoal(.6))
	lookPlayer := NewLookAtPlayerGoal(3)
	lookPlayer.Probability = 1
	ai.Goals.Add(9, lookPlayer)
	lookMob := NewLookAtPlayerGoal(8)
	lookMob.IsType = isMobEntity
	ai.Goals.Add(10, lookMob)
}

// vindicatorBreakDoorGoal ports Vindicator.VindicatorBreakDoorGoal: in raids
// vindicators break wooden doors on normal and hard.
type vindicatorBreakDoorGoal struct{ *BreakDoorGoal }

func (g *vindicatorBreakDoorGoal) Flags() GoalFlag { return GoalMove }

func (g *vindicatorBreakDoorGoal) CanUse(m *Mob) bool {
	return raiderOf(m).activeRaid(m) != nil && m.Random().NextInt(int32(reducedTickDelay(10))) == 0 && g.BreakDoorGoal.CanUse(m)
}

func (g *vindicatorBreakDoorGoal) CanContinueToUse(m *Mob) bool {
	return raiderOf(m).activeRaid(m) != nil && g.BreakDoorGoal.CanContinueToUse(m)
}

func (g *vindicatorBreakDoorGoal) Start(m *Mob) {
	g.BreakDoorGoal.Start(m)
	m.AI.noActionTime = 0
}

// vindicatorJohnnyAttackGoal ports Vindicator.VindicatorJohnnyAttackGoal: a
// vindicator named Johnny attacks every living thing.
type vindicatorJohnnyAttackGoal struct{ *NearestAttackableTargetGoal }

func (g *vindicatorJohnnyAttackGoal) CanUse(m *Mob) bool {
	return hostileOf(m).isJohnny(m) && g.NearestAttackableTargetGoal.CanUse(m)
}

func (g *vindicatorJohnnyAttackGoal) Start(m *Mob) {
	g.NearestAttackableTargetGoal.Start(m)
	m.AI.noActionTime = 0
}

// isJohnny reports if the vindicator is Johnny, by its name or the Johnny
// tag Java saves.
func (b *hostileState) isJohnny(m *Mob) bool { return b.johnny || m.data.Name == "Johnny" }

// customServerAiStep runs the custom AI steps of hostile mobs: vindicators
// open doors only where a raid goes on, and spellcasters count their spell
// down.
func (b *hostileState) customServerAiStep(m *Mob) {
	switch b.kind {
	case "minecraft:vindicator":
		m.AI.nav.SetCanOpenDoors(m.tx.RaidAt(m.BlockPos()) != nil)
	case "minecraft:evoker":
		if b.spellTicks > 0 {
			b.spellTicks--
		}
	}
}

func hostileOf(m *Mob) *hostileState { return m.Behaviour().(*hostileState) }

// entityTypeInTag reports if the type of an entity is in a vanilla entity
// type tag.
func entityTypeInTag(e world.Entity, tag string) bool {
	id := e.H().Type().EncodeEntity()
	if java, ok := bedrockOnlyEntityIDs[id]; ok {
		id = java
	}
	in, _ := gamedata.EntityTypeTagContains(tag, id)
	return in
}

// bedrockOnlyEntityIDs are the Java identifiers of the entities Bedrock
// names otherwise.
var bedrockOnlyEntityIDs = map[string]string{
	"minecraft:villager_v2":        "minecraft:villager",
	"minecraft:zombie_villager_v2": "minecraft:zombie_villager",
	"minecraft:zombie_pigman":      "minecraft:zombified_piglin",
	"minecraft:evocation_illager":  "minecraft:evoker",
}
