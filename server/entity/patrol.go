package entity

import (
	"math"
	"sync"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
)

// patrolSpawners holds each world's PatrolSpawner countdown, which Java
// does not persist either.
var patrolSpawners sync.Map

// patrolSpawnTick ports PatrolSpawner.tick for the overworld.
func patrolSpawnTick(tx *world.Tx, random *vanilla.Random) {
	w := tx.World()
	if w.Dimension() != world.Overworld || w.Difficulty() == world.DifficultyPeaceful || !w.MonsterSpawningEnabled() || !w.GameRuleBool("spawn_patrols") {
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
	return world.JavaBiomeName(tx.Biome(pos)) != "mushroom_fields"
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
		s.findPatrolTarget(cube.PosFromVec3(m.Position()), &s.random)
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
