package entity

import (
	"math"
	"sync"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// patrolTick ports PatrollingMonster.LongDistancePatrolGoal: outside raids,
// a patrolling raider with no target walks toward its patrol target in short
// legs offset sideways; the leader picks a new far-away target on arrival
// and hands each leg's destination to its companions. A raider without
// companions stops patrolling.
func (b *hostileState) patrolTick(e *Ent, tx *world.Tx) bool {
	if !b.patrolling || !b.hasPatrolTarget || b.raidID != 0 || !hostileRaidMob(b.kind) {
		return false
	}
	if b.navigator.Index < len(b.navigator.Path) {
		b.followPath(e, tx, b.patrolSpeed())
		return true
	}
	if b.patrolCooldown > 0 {
		b.patrolCooldown--
		b.stop(e, tx)
		return true
	}
	companions := patrolCompanions(e, tx)
	switch {
	case len(companions) == 0:
		b.patrolling = false
		return false
	case b.patrolLeader && b.patrolTarget.Vec3Centre().Sub(e.Position()).Len() < 10:
		b.findPatrolTarget(e)
	default:
		target := b.patrolTarget.Vec3Middle()
		pos := e.Position()
		// distance.yRot(90).scale(0.4).add(target)
		d := pos.Sub(target)
		target = target.Add(mgl64.Vec3{d[2], 0, -d[0]}.Mul(0.4))
		dir := target.Sub(pos)
		if dir.Len() > 0 {
			dir = dir.Normalize()
		}
		leg := cube.PosFromVec3(pos.Add(dir.Mul(10)))
		leg[1] = tx.HighestBlock(leg[0], leg[2]) + 1
		start := cube.PosFromVec3(pos)
		if !b.navigator.MoveTo(tx, start, leg, GroundProfile{MaxVisitedNodes: 400}, navigatorTick(e)) {
			b.wander, b.patrolCooldown = 0, 200
			b.wanderTick(e, tx)
			return true
		}
		if b.patrolLeader {
			for _, c := range companions {
				c.patrolTarget, c.hasPatrolTarget = leg, true
			}
		}
	}
	b.followPath(e, tx, b.patrolSpeed())
	return true
}

// patrolSpeed returns the leader (0.595) or member (0.7) speed modifier.
func (b *hostileState) patrolSpeed() float64 {
	if b.patrolLeader {
		return 0.595
	}
	return 0.7
}

// followPath advances along the navigator path at the speed modifier.
func (b *hostileState) followPath(e *Ent, tx *world.Tx, modifier float64) {
	p, ok := b.navigator.Tick(e.Position(), navigatorTick(e), .6)
	if !ok {
		b.stop(e, tx)
		return
	}
	if p.Sub(e.Position()).Len() < .3 {
		b.navigator.Index++
		return
	}
	speed := b.speed
	b.speed = speed * modifier
	b.move(e, tx, p)
	b.speed = speed
}

// findPatrolTarget ports PatrollingMonster.findPatrolTarget: a target up to
// 500 blocks away on each horizontal axis.
func (b *hostileState) findPatrolTarget(e *Ent) {
	pos := cube.PosFromVec3(e.Position())
	b.patrolTarget = pos.Add(cube.Pos{-500 + int(b.random.NextInt(1000)), 0, -500 + int(b.random.NextInt(1000))})
	b.hasPatrolTarget, b.patrolling = true, true
}

// patrolCompanions ports LongDistancePatrolGoal.findPatrolCompanions:
// other patrol-capable raiders within 16 blocks.
func patrolCompanions(e *Ent, tx *world.Tx) []*hostileState {
	var out []*hostileState
	for x := range tx.EntitiesWithin(cube.Box(-16, -16, -16, 16, 16, 16).Translate(e.Position())) {
		m, ok := x.(interface{ state() *hostileState })
		if !ok || x.H() == e.H() {
			continue
		}
		if s := m.state(); hostileRaidMob(s.kind) && s.raidID == 0 && !s.dead {
			out = append(out, s)
		}
	}
	return out
}

// patrolSpawners holds each world's PatrolSpawner countdown, which Java
// does not persist either.
var patrolSpawners sync.Map

// patrolSpawnTick ports PatrolSpawner.tick for the overworld.
func patrolSpawnTick(tx *world.Tx, random *java26.Random) {
	w := tx.World()
	if w.Dimension() != world.Overworld || w.Difficulty() == world.DifficultyPeaceful || !w.SpawnMobsEnabled() || !w.GameRuleBool("spawn_patrols") {
		return
	}
	v, _ := patrolSpawners.LoadOrStore(w, new(int))
	next := v.(*int)
	if *next--; *next > 0 {
		return
	}
	*next += 12000 + int(random.NextInt(1200))
	if !brightOutside(tx) || random.NextInt(5) != 0 {
		return
	}
	var players []world.Entity
	for p := range tx.Players() {
		players = append(players, p)
	}
	if len(players) == 0 {
		return
	}
	player := players[random.NextInt(int32(len(players)))]
	if m, ok := player.(interface{ GameMode() world.GameMode }); ok && m.GameMode() == world.GameModeSpectator {
		return
	}
	origin := cube.PosFromVec3(player.Position())
	if tx.VillageDistance(origin) <= 2 {
		return
	}
	sign := func() int {
		if random.NextBoolean() {
			return -1
		}
		return 1
	}
	dx := (24 + int(random.NextInt(24))) * sign()
	dz := (24 + int(random.NextInt(24))) * sign()
	pos := origin.Add(cube.Pos{dx, 0, dz})
	if !patrolBiomeAllowed(tx, pos) {
		return
	}
	group := int(math.Ceil(float64(tx.LocalDifficulty(pos).Effective))) + 1
	for i := range group {
		pos[1] = tx.HighestBlock(pos[0], pos[2]) + 1
		if !spawnPatrolMember(tx, pos, i == 0) && i == 0 {
			break
		}
		pos[0] += int(random.NextInt(5)) - int(random.NextInt(5))
		pos[2] += int(random.NextInt(5)) - int(random.NextInt(5))
	}
}

// patrolBiomeAllowed reports Java's CAN_PILLAGER_PATROL_SPAWN biome
// attribute, which is off only in mushroom fields.
func patrolBiomeAllowed(tx *world.Tx, pos cube.Pos) bool {
	return tx.Biome(pos).String() != "mushroom_island"
}

// spawnPatrolMember ports PatrolSpawner.spawnPatrolMember with
// checkPatrollingMonsterSpawnRules: an empty spawn space above a valid
// surface with block light at most 8.
func spawnPatrolMember(tx *world.Tx, pos cube.Pos, leader bool) bool {
	if len(tx.Block(pos).Model().BBox(pos, tx)) > 0 || len(tx.Block(pos.Side(cube.FaceUp)).Model().BBox(pos.Side(cube.FaceUp), tx)) > 0 {
		return false
	}
	if _, liquid := tx.Liquid(pos); liquid {
		return false
	}
	below := pos.Side(cube.FaceDown)
	if !tx.Block(below).Model().FaceSolid(below, cube.FaceUp, tx) || int(tx.Light(pos))-int(tx.SkyLight(pos)) > 8 {
		return false
	}
	h := NewPillager(world.EntitySpawnOpts{Position: pos.Vec3Middle(), SpawnReason: "patrol"})
	e := tx.AddEntity(h)
	m, ok := e.(*Pillager)
	if !ok {
		return false
	}
	s := m.state()
	if leader {
		s.patrolLeader = true
		s.findPatrolTarget(m.Ent)
	}
	m.FinalizeSpawn("patrol", &s.random)
	return true
}

// brightOutside ports Level.isBrightOutside: the sky darkening from the
// time of day, rain and thunder stays below 4.
func brightOutside(tx *world.Tx) bool {
	w := tx.World()
	if !w.Dimension().TimeCycle() {
		return false
	}
	d := math.Mod(float64(w.Time())/24000-0.25, 1)
	if d < 0 {
		d++
	}
	timeOfDay := (d*2 + (0.5 - math.Cos(d*math.Pi)/2)) / 3
	v := 1 - (math.Cos(timeOfDay*2*math.Pi)*2 + 0.5)
	v = 1 - min(max(v, 0), 1)
	if tx.Raining() {
		v *= 1 - 5.0/16
	}
	if tx.Thundering() {
		v *= 1 - 5.0/16
	}
	return int((1-v)*11) < 4
}
