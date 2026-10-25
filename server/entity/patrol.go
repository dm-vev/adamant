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
	// Level.hasChunksAt: the chunks within ten blocks are loaded.
	for _, corner := range [4][2]int{{-10, -10}, {10, -10}, {-10, 10}, {10, 10}} {
		if !tx.ChunkLoaded(world.ChunkPos{int32((pos[0] + corner[0]) >> 4), int32((pos[2] + corner[1]) >> 4)}) {
			return
		}
	}
	if !patrolBiomeAllowed(tx, pos) {
		return
	}
	group := int(math.Ceil(float64(tx.LocalDifficulty(pos).Effective))) + 1
	for i := range group {
		// The MOTION_BLOCKING_NO_LEAVES height map: under leaves and on
		// top of grass.
		pos[1] = tx.JavaMotionBlockingHeight(pos[0], pos[2], true)
		if !spawnPatrolMember(tx, pos, i == 0, random) && i == 0 {
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

// spawnPatrolMember ports PatrolSpawner.spawnPatrolMember: a pillager spawns
// in an empty spawn block where checkPatrollingMonsterSpawnRules lets it,
// leading the patrol if it is the first, and is then initialised once with
// the patrol spawn reason.
func spawnPatrolMember(tx *world.Tx, pos cube.Pos, leader bool, random *vanilla.Random) bool {
	const id = "minecraft:pillager"
	if !validEmptySpawnBlock(tx, pos, id) {
		return false
	}
	// PatrollingMonster.checkPatrollingMonsterSpawnRules.
	if tx.BlockLight(pos) > 8 || !checkMobSpawnRules(tx, id, pos) {
		return false
	}
	h := NewPillager(world.EntitySpawnOpts{Position: pos.Vec3(), SpawnReason: "patrol", SpawnRandom: random})
	if m, ok := h.WorldlessEntity(tx).(*Pillager); ok && leader {
		s := m.state()
		s.patrolLeader = true
		s.findPatrolTarget(pos, &s.random)
	}
	tx.AddEntity(h)
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
