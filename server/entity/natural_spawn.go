package entity

import (
	"cmp"
	"math"
	"slices"
	"sync/atomic"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/loot"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// This file ports NaturalSpawner and the spawning half of
// ServerChunkCache.tickChunks.

// spawningCategories are MobCategory's spawning categories, in its order.
var spawningCategories = [...]naturalSpawnCategory{spawnMonster, spawnCreature, spawnAmbient, spawnAxolotls, spawnUndergroundWaterCreature, spawnWaterCreature, spawnWaterAmbient}

// maxInstancesPerChunk is MobCategory.getMaxInstancesPerChunk.
func maxInstancesPerChunk(c naturalSpawnCategory) int {
	switch c {
	case spawnMonster:
		return 70
	case spawnCreature:
		return 10
	case spawnAmbient:
		return 15
	case spawnWaterAmbient:
		return 20
	case spawnMisc:
		return -1
	}
	return 5
}

// spawnCategoryName is the name biome spawn settings give a category.
func spawnCategoryName(c naturalSpawnCategory) string {
	return [...]string{"creature", "monster", "ambient", "water_creature", "water_ambient", "underground_water_creature", "axolotls", "misc"}[c]
}

// spawnCategoryMagicNumber is NaturalSpawner.MAGIC_NUMBER, the chunks around
// one player: a mob cap counts per 17 by 17 chunks.
const spawnCategoryMagicNumber = 17 * 17

// pointCharge is a PotentialCalculator.PointCharge.
type pointCharge struct {
	pos    cube.Pos
	charge float64
}

// spawnState ports NaturalSpawner.SpawnState with its LocalMobCapCalculator.
type spawnState struct {
	tx            *world.Tx
	players       []world.Entity
	chunkCount    int
	counts        [8]int
	charges       []pointCharge
	near          map[world.ChunkPos][]int
	playerCounts  [][8]int
	lastPos       cube.Pos
	lastType      string
	lastCharge    float64
	lastValid     bool
	fortressEnemy []world.NaturalSpawnEntry
	// grid holds the indices of the players in cells of playerCell blocks,
	// so that the players near a position are found among few.
	grid map[[2]int32][]int
}

// playerCell is the size of the cells of spawnState.grid: the 128 blocks
// within which players count for spawning, so that every player that near
// a position is in its cell or one beside it.
const playerCell = 128

// playerCellOf returns the cell of the grid holding x and z.
func playerCellOf(x, z float64) [2]int32 {
	return [2]int32{int32(math.Floor(x / playerCell)), int32(math.Floor(z / playerCell))}
}

// playersAround calls f with the index of each player in the cell of x and
// z or a cell beside it.
func (s *spawnState) playersAround(x, z float64, f func(i int)) {
	c := playerCellOf(x, z)
	for dx := int32(-1); dx <= 1; dx++ {
		for dz := int32(-1); dz <= 1; dz++ {
			for _, i := range s.grid[[2]int32{c[0] + dx, c[1] + dz}] {
				f(i)
			}
		}
	}
}

// spawnNaturally ports ServerChunkCache.tickChunks for natural spawning: each
// chunk within eight chunks of a player and 128 blocks of one spawns the
// categories of mobs that are under their caps.
func spawnNaturally(tx *world.Tx, players []world.Entity, r *vanilla.Random) {
	// DistanceManager.naturalSpawnChunkCounter: the chunks within eight of
	// a player, spectators included while they generate chunks.
	trackers := players
	if tx.World().GameRuleBool("spectators_generate_chunks") {
		trackers = nil
		for p := range tx.Players() {
			trackers = append(trackers, p)
		}
	}
	trackerRuns := chunkRunsAround(trackers, 8)
	chunkCount := 0
	for _, run := range trackerRuns {
		chunkCount += int(run.z1-run.z0) + 1
	}
	state := newSpawnState(tx, players, chunkCount)
	w := tx.World()
	// ServerChunkCache.spawnEnemies follows the spawn_monsters rule alone:
	// on peaceful the spawn rules of each kind turn monsters away, and
	// hoglins and piglins still spawn.
	enemies := w.MonsterSpawningEnabled()
	persistent := tx.CurrentTick()%400 == 0
	var categories []naturalSpawnCategory
	for _, c := range spawningCategories {
		friendly, isPersistent := c != spawnMonster, c == spawnCreature
		if (enemies || friendly) && (persistent || !isPersistent) && state.canSpawnGlobal(c) {
			categories = append(categories, c)
		}
	}
	// A chunk with a player within 128 blocks of its centre is within eight
	// chunks of the player, so the chunks around the players spawning hold
	// every chunk the trackers' do that may spawn mobs.
	runs := trackerRuns
	if len(trackers) != len(players) {
		runs = chunkRunsAround(players, 8)
	}
	chunks := state.spawningChunks(runs)
	// Util.shuffle.
	for i := len(chunks); i > 1; i-- {
		j := int(r.NextInt(int32(i)))
		chunks[i-1], chunks[j] = chunks[j], chunks[i-1]
	}
	if len(categories) == 0 {
		return
	}
	for _, chunk := range chunks {
		for _, c := range categories {
			if state.canSpawnLocal(c, chunk.near) {
				state.spawnCategoryForChunk(c, chunk.pos, r)
			}
		}
	}
}

// chunkRun is a run of chunks in the column of chunk X x, from chunk Z z0
// to z1.
type chunkRun struct{ x, z0, z1 int32 }

// chunkOf returns the chunk holding the entity passed.
func chunkOf(e world.Entity) world.ChunkPos {
	pos := e.Position()
	return world.ChunkPos{int32(math.Floor(pos[0])) >> 4, int32(math.Floor(pos[2])) >> 4}
}

// chunkRunsAround returns the chunks within r chunks of the chunks of the
// entities passed, as runs that do not overlap, in order of X and then Z.
func chunkRunsAround(entities []world.Entity, r int32) []chunkRun {
	centres := make([]world.ChunkPos, 0, len(entities))
	for _, e := range entities {
		centres = append(centres, chunkOf(e))
	}
	slices.SortFunc(centres, compareChunks)
	centres = slices.Compact(centres)
	runs := make([]chunkRun, 0, len(centres)*int(2*r+1))
	for _, c := range centres {
		for x := c[0] - r; x <= c[0]+r; x++ {
			runs = append(runs, chunkRun{x: x, z0: c[1] - r, z1: c[1] + r})
		}
	}
	slices.SortFunc(runs, func(a, b chunkRun) int {
		return cmp.Or(cmp.Compare(a.x, b.x), cmp.Compare(a.z0, b.z0))
	})
	merged := runs[:0]
	for _, run := range runs {
		if n := len(merged); n > 0 && merged[n-1].x == run.x && run.z0 <= merged[n-1].z1+1 {
			merged[n-1].z1 = max(merged[n-1].z1, run.z1)
			continue
		}
		merged = append(merged, run)
	}
	return merged
}

// compareChunks orders chunks by X and then Z.
func compareChunks(a, b world.ChunkPos) int {
	return cmp.Or(cmp.Compare(a[0], b[0]), cmp.Compare(a[1], b[1]))
}

// spawnChunk is a chunk mobs may spawn in and the indices of the players
// near it.
type spawnChunk struct {
	pos  world.ChunkPos
	near []int
}

// spawningChunks ports ChunkMap.collectSpawningChunks with the check of
// ServerLevel.canSpawnEntitiesInChunk: the entity-ticking chunks of the runs
// passed with players near, in the order of the runs. The players near each
// are found among those within eight chunks of it, which are swept along
// the runs.
func (s *spawnState) spawningChunks(runs []chunkRun) []spawnChunk {
	n := len(s.players)
	chunkX, chunkZ := make([]int32, n), make([]int32, n)
	posX, posZ := make([]float64, n), make([]float64, n)
	byX := make([]int, n)
	for i, p := range s.players {
		c, pos := chunkOf(p), p.Position()
		chunkX[i], chunkZ[i], posX[i], posZ[i], byX[i] = c[0], c[1], pos[0], pos[2], i
	}
	slices.SortStableFunc(byX, func(a, b int) int { return cmp.Compare(chunkX[a], chunkX[b]) })
	var (
		chunks []spawnChunk
		// ends holds the end of the players of each chunk in near.
		ends, near []int
		// column holds the players within eight chunk columns of the run,
		// by chunk Z.
		column     []int
		columnX    int32
		start, end int
	)
	for k, run := range runs {
		if k == 0 || run.x != columnX {
			columnX = run.x
			for end < n && chunkX[byX[end]] <= columnX+8 {
				end++
			}
			for start < end && chunkX[byX[start]] < columnX-8 {
				start++
			}
			column = append(column[:0], byX[start:end]...)
			slices.SortStableFunc(column, func(a, b int) int { return cmp.Compare(chunkZ[a], chunkZ[b]) })
		}
		lo, hi := 0, 0
		for z := run.z0; z <= run.z1; z++ {
			for hi < len(column) && chunkZ[column[hi]] <= z+8 {
				hi++
			}
			for lo < hi && chunkZ[column[lo]] < z-8 {
				lo++
			}
			first := len(near)
			x, zz := float64(run.x)*16+8, float64(z)*16+8
			for _, i := range column[lo:hi] {
				if dx, dz := x-posX[i], zz-posZ[i]; dx*dx+dz*dz < playerCell*playerCell {
					near = append(near, i)
				}
			}
			if len(near) == first {
				continue
			}
			if pos := (world.ChunkPos{run.x, z}); s.tx.EntityTicking(pos) {
				chunks = append(chunks, spawnChunk{pos: pos})
				ends = append(ends, len(near))
				continue
			}
			near = near[:first]
		}
	}
	first := 0
	for i := range chunks {
		chunks[i].near, first = near[first:ends[i]:ends[i]], ends[i]
	}
	return chunks
}

// newSpawnState ports NaturalSpawner.createState: the mobs that may despawn
// count towards the caps of their categories, near which players they are,
// and the spawn potential of the mobs with spawn costs.
func newSpawnState(tx *world.Tx, players []world.Entity, chunkCount int) *spawnState {
	s := &spawnState{tx: tx, players: players, chunkCount: chunkCount, near: map[world.ChunkPos][]int{}, playerCounts: make([][8]int, len(players)), grid: map[[2]int32][]int{}}
	for i, p := range players {
		c := playerCellOf(p.Position()[0], p.Position()[2])
		s.grid[c] = append(s.grid[c], i)
	}
	for e := range tx.EntitiesOfType(countsTowardsCaps) {
		id := loot.JavaEntityID(e)
		category, ok := mobCategories[id]
		if !ok {
			continue
		}
		m, isMob := mobOf(e)
		if isMob && (m.AI.persistent || mobRequiresCustomPersistence(m)) {
			continue
		}
		pos := cube.PosFromVec3(e.Position())
		chunk := world.ChunkPos{int32(pos[0] >> 4), int32(pos[2] >> 4)}
		if !tx.ChunkLoaded(chunk) {
			continue
		}
		if !world.HasJavaSpawnCost(id) {
			// No biome gives the mob a spawn cost.
		} else if cost, ok := biomeSpawnsOf(tx.Biome(pos)).costs[id]; ok {
			s.charges = append(s.charges, pointCharge{pos: pos, charge: cost.Charge})
		}
		if isMob {
			s.addLocal(chunk, category)
		}
		s.counts[category]++
	}
	return s
}

// countsTowardsCaps reports whether the entities of a type are mobs of a
// spawning category, which may count towards its caps.
func countsTowardsCaps(t world.EntityType) bool {
	_, ok := mobCategories[loot.JavaEntityTypeID(t.EncodeEntity())]
	return ok
}

// playersNear ports ChunkMap.getPlayersCloseForSpawning: the players within
// 128 blocks of the centre of the chunk.
func (s *spawnState) playersNear(pos world.ChunkPos) []int {
	if near, ok := s.near[pos]; ok {
		return near
	}
	var near []int
	x, z := float64(pos[0])*16+8, float64(pos[1])*16+8
	s.playersAround(x, z, func(i int) {
		p := s.players[i]
		dx, dz := x-p.Position()[0], z-p.Position()[2]
		if dx*dx+dz*dz < playerCell*playerCell {
			near = append(near, i)
		}
	})
	s.near[pos] = near
	return near
}

// addLocal ports LocalMobCapCalculator.addMob.
func (s *spawnState) addLocal(pos world.ChunkPos, c naturalSpawnCategory) {
	for _, i := range s.playersNear(pos) {
		s.playerCounts[i][c]++
	}
}

// canSpawnGlobal ports SpawnState.canSpawnForCategoryGlobal.
func (s *spawnState) canSpawnGlobal(c naturalSpawnCategory) bool {
	return s.counts[c] < maxInstancesPerChunk(c)*s.chunkCount/spawnCategoryMagicNumber
}

// canSpawnLocal ports LocalMobCapCalculator.canSpawn: a player near the chunk,
// of those whose indices are passed, has room left for a mob of the
// category.
func (s *spawnState) canSpawnLocal(c naturalSpawnCategory, near []int) bool {
	for _, i := range near {
		if s.playerCounts[i][c] < maxInstancesPerChunk(c) {
			return true
		}
	}
	return false
}

// canSpawn ports SpawnState.canSpawn: a mob with a spawn cost spawns only if
// it adds little enough to the spawn potential.
func (s *spawnState) canSpawn(id string, pos cube.Pos) bool {
	s.lastPos, s.lastType, s.lastValid = pos, id, true
	if !world.HasJavaSpawnCost(id) {
		s.lastCharge = 0
		return true
	}
	cost, ok := biomeSpawnsOf(s.tx.Biome(pos)).costs[id]
	if !ok {
		s.lastCharge = 0
		return true
	}
	s.lastCharge = cost.Charge
	return s.potentialChange(pos, cost.Charge) <= cost.EnergyBudget
}

// potentialChange ports PotentialCalculator.getPotentialEnergyChange.
func (s *spawnState) potentialChange(pos cube.Pos, charge float64) float64 {
	change := 0.
	for _, c := range s.charges {
		dx, dy, dz := float64(c.pos[0]-pos[0]), float64(c.pos[1]-pos[1]), float64(c.pos[2]-pos[2])
		dist := dx*dx + dy*dy + dz*dz
		if dist == 0 {
			change += math.Inf(1)
			continue
		}
		change += c.charge / math.Sqrt(dist)
	}
	return change * charge
}

// afterSpawn ports SpawnState.afterSpawn.
func (s *spawnState) afterSpawn(id string, pos cube.Pos) {
	charge := 0.
	if s.lastValid && pos == s.lastPos && id == s.lastType {
		charge = s.lastCharge
	} else if !world.HasJavaSpawnCost(id) {
		// No biome gives the mob a spawn cost.
	} else if cost, ok := biomeSpawnsOf(s.tx.Biome(pos)).costs[id]; ok {
		charge = cost.Charge
	}
	// A mob without a spawn cost adds a charge of zero, which still keeps
	// mobs with a cost from spawning in its very block.
	s.charges = append(s.charges, pointCharge{pos: pos, charge: charge})
	c := mobCategories[id]
	s.counts[c]++
	s.addLocal(world.ChunkPos{int32(pos[0] >> 4), int32(pos[2] >> 4)}, c)
}

// spawnCategoryForChunk ports NaturalSpawner.spawnCategoryForChunk: a group
// spawns around a random position in the chunk below its surface.
func (s *spawnState) spawnCategoryForChunk(c naturalSpawnCategory, chunk world.ChunkPos, r *vanilla.Random) {
	tx := s.tx
	x, z := int(chunk[0])*16+int(r.NextInt(16)), int(chunk[1])*16+int(r.NextInt(16))
	minY := tx.Range()[0]
	// The WORLD_SURFACE height map: the first empty block above the highest
	// block, or the bottom of an empty column.
	top := tx.HighestBlock(x, z) + 1
	if top == minY+1 {
		if _, air := tx.Block(cube.Pos{x, minY, z}).(block.Air); air {
			top = minY
		}
	}
	y := minY + int(r.NextInt(int32(top-minY+1)))
	if y >= minY+1 {
		s.spawnCategoryForPosition(c, chunk, cube.Pos{x, y, z}, r)
	}
}

// spawnCategoryForPosition ports NaturalSpawner.spawnCategoryForPosition: up
// to three groups of the same kind of mob spawn around a position, each
// mob a few blocks from the last, until the kind's cluster size is reached.
func (s *spawnState) spawnCategoryForPosition(c naturalSpawnCategory, chunk world.ChunkPos, start cube.Pos, r *vanilla.Random) {
	tx := s.tx
	if redstoneConductorAt(tx, start) {
		return
	}
	spawn := tx.World().EntityRegistry().Config().SpawnMob
	if spawn == nil {
		return
	}
	clusterSize := 0
	for range 3 {
		x, z := start[0], start[2]
		var data *world.NaturalSpawnEntry
		group := &world.EntitySpawnGroup{}
		maxCount := int(math.Ceil(float64(r.NextFloat() * 4)))
		groupSize := 0
		for ll := 0; ll < maxCount; ll++ {
			x += int(r.NextInt(6)) - int(r.NextInt(6))
			z += int(r.NextInt(6)) - int(r.NextInt(6))
			pos := cube.Pos{x, start[1], z}
			xx, zz := float64(x)+.5, float64(z)+.5
			dist, ok := s.nearestPlayerDistSqr(mgl64.Vec3{xx, float64(start[1]), zz})
			if !ok || !s.rightDistanceToPlayerAndSpawn(chunk, pos, dist) {
				continue
			}
			if data == nil {
				next, ok := s.randomSpawnMobAt(c, pos, r)
				if !ok {
					break
				}
				data = next
				maxCount = sampleSpawnCount(*data, r)
			}
			if !s.validSpawnPositionForType(c, *data, pos, dist, r) || !s.canSpawn(data.ID, pos) {
				continue
			}
			// Mob.finalizeSpawn draws from the level random the spawner
			// draws from.
			opts := world.EntitySpawnOpts{Position: mgl64.Vec3{xx, float64(start[1]), zz}, Rotation: cube.Rotation{float64(r.NextFloat() * 360), 0},
				SpawnReason: "natural", SpawnRandom: r, SpawnGroup: group}
			handle := spawn(data.ID, opts)
			if handle == nil {
				return
			}
			if !validPositionForMob(tx, handle, data.ID, pos, dist) {
				continue
			}
			e := tx.AddEntity(handle)
			clusterSize++
			groupSize++
			s.afterSpawn(data.ID, pos)
			if clusterSize >= maxSpawnClusterSize(data.ID) {
				return
			}
			if g, ok := e.(interface{ maxGroupSizeReached(int) bool }); ok && g.maxGroupSizeReached(groupSize) {
				break
			}
		}
	}
}

// sampleSpawnCount ports SpawnerData.count().sample: a constant count draws
// nothing.
func sampleSpawnCount(e world.NaturalSpawnEntry, r *vanilla.Random) int {
	if e.MinCount == e.MaxCount {
		return e.MinCount
	}
	return e.MinCount + int(r.NextInt(int32(e.MaxCount-e.MinCount+1)))
}

// nearestPlayerDistSqr returns the squared distance to the nearest player
// that is not spectating.
func (s *spawnState) nearestPlayerDistSqr(pos mgl64.Vec3) (float64, bool) {
	best, found := 0., false
	s.playersAround(pos[0], pos[2], func(i int) {
		if d := s.players[i].Position().Sub(pos).LenSqr(); !found || d < best {
			best, found = d, true
		}
	})
	if found && best < playerCell*playerCell {
		// Every player outside the cells around is further than a cell.
		return best, true
	}
	best, found = 0., false
	for _, p := range s.players {
		if d := p.Position().Sub(pos).LenSqr(); !found || d < best {
			best, found = d, true
		}
	}
	return best, found
}

// rightDistanceToPlayerAndSpawn ports
// NaturalSpawner.isRightDistanceToPlayerAndSpawnPoint: mobs spawn more than
// 24 blocks from players and the world spawn, in entity-ticking chunks.
func (s *spawnState) rightDistanceToPlayerAndSpawn(chunk world.ChunkPos, pos cube.Pos, dist float64) bool {
	if dist <= 576 {
		return false
	}
	// The world spawn keeps mobs away only in the dimension it lies in, the
	// Overworld.
	if w := s.tx.World(); w.Dimension() == world.Overworld {
		if spawn := w.Spawn(); spawn.Vec3Centre().Sub(mgl64.Vec3{float64(pos[0]) + .5, float64(pos[1]), float64(pos[2]) + .5}).LenSqr() < 24*24 {
			return false
		}
	}
	at := world.ChunkPos{int32(pos[0] >> 4), int32(pos[2] >> 4)}
	return at == chunk || s.tx.EntityTicking(at)
}

// randomSpawnMobAt ports NaturalSpawner.getRandomSpawnMobAt.
func (s *spawnState) randomSpawnMobAt(c naturalSpawnCategory, pos cube.Pos, r *vanilla.Random) (*world.NaturalSpawnEntry, bool) {
	if c == spawnWaterAmbient && biomeHasTag(s.tx, pos, "reduced_water_ambient_spawns") && r.NextFloat() < .98 {
		return nil, false
	}
	rows := s.mobsAt(c, pos)
	total := 0
	for _, e := range rows {
		total += e.Weight
	}
	if total <= 0 {
		return nil, false
	}
	pick := int(r.NextInt(int32(total)))
	for i := range rows {
		if pick -= rows[i].Weight; pick < 0 {
			return &rows[i], true
		}
	}
	return nil, false
}

// fortressEnemies are NetherFortressStructure.FORTRESS_ENEMIES.
var fortressEnemies = []world.NaturalSpawnEntry{
	{ID: "minecraft:blaze", Category: "monster", Weight: 10, MinCount: 2, MaxCount: 3},
	{ID: "minecraft:zombified_piglin", Category: "monster", Weight: 5, MinCount: 4, MaxCount: 4},
	{ID: "minecraft:wither_skeleton", Category: "monster", Weight: 8, MinCount: 5, MaxCount: 5},
	{ID: "minecraft:skeleton", Category: "monster", Weight: 2, MinCount: 5, MaxCount: 5},
	{ID: "minecraft:magma_cube", Category: "monster", Weight: 3, MinCount: 4, MaxCount: 4},
}

// structureSpawnOverride is a StructureSpawnOverride.
type structureSpawnOverride struct {
	pieces bool
	spawns []world.NaturalSpawnEntry
}

// structureSpawnOverrides are the spawn_overrides of the structures that have
// them.
var structureSpawnOverrides = map[string]map[naturalSpawnCategory]structureSpawnOverride{
	"minecraft:ancient_city":   allCategoriesEmpty(false),
	"minecraft:trial_chambers": allCategoriesEmpty(true),
	"minecraft:fortress":       {spawnMonster: {pieces: true, spawns: fortressEnemies}},
	"minecraft:monument": {
		spawnAxolotls: {}, spawnUndergroundWaterCreature: {},
		spawnMonster: {spawns: []world.NaturalSpawnEntry{{ID: "minecraft:guardian", Category: "monster", Weight: 1, MinCount: 2, MaxCount: 4}}},
	},
	"minecraft:pillager_outpost": {spawnMonster: {spawns: []world.NaturalSpawnEntry{{ID: "minecraft:pillager", Category: "monster", Weight: 1, MinCount: 1, MaxCount: 1}}}},
	"minecraft:swamp_hut": {
		spawnCreature: {pieces: true, spawns: []world.NaturalSpawnEntry{{ID: "minecraft:cat", Category: "creature", Weight: 1, MinCount: 1, MaxCount: 1}}},
		spawnMonster:  {pieces: true, spawns: []world.NaturalSpawnEntry{{ID: "minecraft:witch", Category: "monster", Weight: 1, MinCount: 1, MaxCount: 1}}},
	},
}

func allCategoriesEmpty(pieces bool) map[naturalSpawnCategory]structureSpawnOverride {
	m := map[naturalSpawnCategory]structureSpawnOverride{}
	for _, c := range spawningCategories {
		m[c] = structureSpawnOverride{pieces: pieces}
	}
	return m
}

// mobsAt ports NaturalSpawner.mobsAt and ChunkGenerator.getMobsAt: the
// spawns of a fortress or another structure the position is in, or else of
// its biome.
func (s *spawnState) mobsAt(c naturalSpawnCategory, pos cube.Pos) []world.NaturalSpawnEntry {
	tx := s.tx
	areas := tx.World().SpawnStructures(world.ChunkPos{int32(pos[0] >> 4), int32(pos[2] >> 4)})
	if c == spawnMonster {
		if _, bricks := tx.Block(pos.Side(cube.FaceDown)).(block.NetherBricks); bricks {
			for _, a := range areas {
				if a.ID == "minecraft:fortress" && a.Contains(pos, false) {
					return fortressEnemies
				}
			}
		}
	}
	for _, a := range areas {
		override, ok := structureSpawnOverrides[a.ID][c]
		if ok && a.Contains(pos, override.pieces) {
			return override.spawns
		}
	}
	return biomeSpawnsOf(tx.Biome(pos)).spawns[c]
}

// biomeSpawns holds the natural spawns of a biome by category and its spawn
// costs by entity type.
type biomeSpawns struct {
	spawns [8][]world.NaturalSpawnEntry
	costs  map[string]world.NaturalSpawnCost
}

// biomeSpawnCache holds the biomeSpawns of the biomes by their IDs, as the
// spawns of a biome are asked of positions in every chunk every tick.
var biomeSpawnCache [512]atomic.Pointer[biomeSpawns]

// biomeSpawnsOf returns the natural spawns and spawn costs of the biome
// passed. The spawns returned must not be changed.
func biomeSpawnsOf(b world.Biome) *biomeSpawns {
	id := b.EncodeBiome()
	cached := id >= 0 && id < len(biomeSpawnCache)
	if cached {
		if spawns := biomeSpawnCache[id].Load(); spawns != nil {
			return spawns
		}
	}
	name := world.JavaBiomeName(b)
	spawns := &biomeSpawns{costs: world.JavaBiomeSpawnCosts(name)}
	if rows, err := world.JavaBiomeNaturalSpawns(name); err == nil {
		for c := range spawns.spawns {
			category := spawnCategoryName(naturalSpawnCategory(c))
			for _, e := range rows {
				if e.Category == category {
					spawns.spawns[c] = append(spawns.spawns[c], e)
				}
			}
		}
	}
	if cached {
		biomeSpawnCache[id].Store(spawns)
	}
	return spawns
}

// validSpawnPositionForType ports NaturalSpawner.isValidSpawnPostitionForType.
func (s *spawnState) validSpawnPositionForType(c naturalSpawnCategory, data world.NaturalSpawnEntry, pos cube.Pos, dist float64, r *vanilla.Random) bool {
	tx := s.tx
	category, ok := mobCategories[data.ID]
	if !ok || category == spawnMisc {
		return false
	}
	far := despawnDistance(data.ID)
	if data.ID != "minecraft:pillager" && data.ID != "minecraft:shulker" && dist > far*far {
		return false
	}
	if !slices.Contains(s.mobsAt(c, pos), data) || !spawnPositionOk(tx, data.ID, pos) || !checkSpawnRules(tx, data.ID, pos, r) {
		return false
	}
	t, ok := tx.World().EntityRegistry().Lookup(data.ID)
	if !ok {
		return false
	}
	return spawnNoCollision(tx, t.BBox(nil).Translate(mgl64.Vec3{float64(pos[0]) + .5, float64(pos[1]), float64(pos[2]) + .5}))
}

// validPositionForMob ports NaturalSpawner.isValidPositionForMob for a mob
// made but not yet in the world.
func validPositionForMob(tx *world.Tx, handle *world.EntityHandle, id string, pos cube.Pos, dist float64) bool {
	e := handle.WorldlessEntity(tx)
	m, ok := mobOf(e)
	if !ok {
		return true
	}
	far := despawnDistance(id)
	if dist > far*far && mobRemoveWhenFarAway(m, id, dist) {
		return false
	}
	box := handle.Type().BBox(e).Translate(e.Position())
	return mobSpawnWalkTarget(m, id, pos) && mobSpawnObstruction(tx, id, box)
}
