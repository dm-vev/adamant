package entity

import (
	"math"
	"slices"

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
	spawnable := map[world.ChunkPos]struct{}{}
	for _, p := range trackers {
		cx, cz := int32(math.Floor(p.Position()[0]))>>4, int32(math.Floor(p.Position()[2]))>>4
		for dx := int32(-8); dx <= 8; dx++ {
			for dz := int32(-8); dz <= 8; dz++ {
				spawnable[world.ChunkPos{cx + dx, cz + dz}] = struct{}{}
			}
		}
	}
	state := newSpawnState(tx, players, len(spawnable))
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
	chunks := make([]world.ChunkPos, 0, len(spawnable))
	for pos := range spawnable {
		if tx.ChunkLoaded(pos) && len(state.playersNear(pos)) > 0 {
			chunks = append(chunks, pos)
		}
	}
	slices.SortFunc(chunks, func(a, b world.ChunkPos) int {
		if a[0] != b[0] {
			return int(a[0] - b[0])
		}
		return int(a[1] - b[1])
	})
	// Util.shuffle.
	for i := len(chunks); i > 1; i-- {
		j := int(r.NextInt(int32(i)))
		chunks[i-1], chunks[j] = chunks[j], chunks[i-1]
	}
	if len(categories) == 0 {
		return
	}
	for _, pos := range chunks {
		if !tx.EntityTicking(pos) {
			continue
		}
		for _, c := range categories {
			if state.canSpawnLocal(c, pos) {
				state.spawnCategoryForChunk(c, pos, r)
			}
		}
	}
}

// newSpawnState ports NaturalSpawner.createState: the mobs that may despawn
// count towards the caps of their categories, near which players they are,
// and the spawn potential of the mobs with spawn costs.
func newSpawnState(tx *world.Tx, players []world.Entity, chunkCount int) *spawnState {
	s := &spawnState{tx: tx, players: players, chunkCount: chunkCount, near: map[world.ChunkPos][]int{}, playerCounts: make([][8]int, len(players))}
	for e := range tx.Entities() {
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
		if cost, ok := world.JavaBiomeSpawnCosts(world.JavaBiomeName(tx.Biome(pos)))[id]; ok {
			s.charges = append(s.charges, pointCharge{pos: pos, charge: cost.Charge})
		}
		if isMob {
			s.addLocal(chunk, category)
		}
		s.counts[category]++
	}
	return s
}

// playersNear ports ChunkMap.getPlayersCloseForSpawning: the players within
// 128 blocks of the centre of the chunk.
func (s *spawnState) playersNear(pos world.ChunkPos) []int {
	if near, ok := s.near[pos]; ok {
		return near
	}
	var near []int
	x, z := float64(pos[0])*16+8, float64(pos[1])*16+8
	for i, p := range s.players {
		dx, dz := x-p.Position()[0], z-p.Position()[2]
		if dx*dx+dz*dz < 16384 {
			near = append(near, i)
		}
	}
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

// canSpawnLocal ports LocalMobCapCalculator.canSpawn: a player near the chunk
// has room left for a mob of the category.
func (s *spawnState) canSpawnLocal(c naturalSpawnCategory, pos world.ChunkPos) bool {
	for _, i := range s.playersNear(pos) {
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
	cost, ok := world.JavaBiomeSpawnCosts(world.JavaBiomeName(s.tx.Biome(pos)))[id]
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
	} else if cost, ok := world.JavaBiomeSpawnCosts(world.JavaBiomeName(s.tx.Biome(pos)))[id]; ok {
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
	rows, err := world.JavaBiomeNaturalSpawns(world.JavaBiomeName(tx.Biome(pos)))
	if err != nil {
		return nil
	}
	name := spawnCategoryName(c)
	out := make([]world.NaturalSpawnEntry, 0, len(rows))
	for _, e := range rows {
		if e.Category == name {
			out = append(out, e)
		}
	}
	return out
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
