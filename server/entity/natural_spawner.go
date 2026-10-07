package entity

import (
	"math"
	"errors"
	"strings"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	java26 "github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

type naturalSpawnCategory uint8

const (
	spawnCreature naturalSpawnCategory = iota
	spawnMonster
	spawnAmbient
	spawnWaterCreature
	spawnWaterAmbient
	spawnUndergroundWaterCreature
	spawnAxolotls
	spawnMisc
)

// NaturalSpawnTick is invoked on the world owner every game tick. It uses the
// Java legacy random stream independently of terrain and decoration generation.
func NaturalSpawnTick(tx *world.Tx) {
	w := tx.World()
	players := make([]world.Entity, 0, 4)
	for p := range tx.Players() {
		mode, ok := p.(interface{ GameMode() world.GameMode })
		if !ok || mode.GameMode() != world.GameModeSpectator {
			players = append(players, p)
		}
	}
	if len(players) == 0 {
		naturalDespawn(tx)
		return
	}
	seed := w.Seed() ^ int64(uint64(tx.CurrentTick())*0x9e3779b97f4a7c15)
	random := java26.NewLegacyRandom(seed)
	counts := [8]int{}
	for e := range tx.Entities() {
		if _, living := e.(Living); living {
			if category, ok := naturalCategory(e.H().Type().EncodeEntity()); ok {
				counts[category]++
			}
		}
	}
	maxPerCategory := [8]int{10, 70, 15, 5, 20, 5, 5, 10}
	for i := range maxPerCategory {
		maxPerCategory[i] *= len(players)
	}
	if w.Difficulty() == world.DifficultyPeaceful {
		maxPerCategory[spawnMonster] = 0
	}
	for _, player := range players {
		origin := player.Position()
		angle := random.NextDouble() * 6.283185307179586
		distance := 24 + int(random.NextInt(104))
		pos := cube.Pos{int(origin[0] + float64(distance)*math.Cos(angle)), 0, int(origin[2] + float64(distance)*math.Sin(angle))}
		chunkPos := world.ChunkPos{int32(pos[0] >> 4), int32(pos[2] >> 4)}
		if !tx.ChunkLoaded(chunkPos) {
			continue
		}
		colY := tx.HighestBlock(pos[0], pos[2]) + 1
		if colY < tx.Range()[0] || colY+2 > tx.Range()[1] {
			continue
		}
		pos[1] = tx.Range()[0] + int(random.NextInt(int32(colY-tx.Range()[0])))
		rows, err := world.JavaBiomeNaturalSpawns(tx.Biome(pos).String())
		if err != nil {
			if !errors.Is(err, world.ErrJavaBiomeNaturalSpawnsUnavailable) {
				panic(err)
			}
			continue
		}
		if random.NextInt(4) != 0 {
			continue
		}
		entry := chooseNaturalEntry(&random, rows, counts[:], maxPerCategory[:])
		if entry == nil {
			continue
		}
		category := naturalSpawnCategoryFor(entry.Category)
		if isNaturalWaterCategory(category) {
			found := false
			for y := pos[1]; y >= max(tx.Range()[0], pos[1]-32); y-- {
				candidate := cube.Pos{pos[0], y, pos[2]}
				if _, ok := tx.Liquid(candidate); ok {
					pos, found = candidate, true
					break
				}
			}
			if !found {
				continue
			}
		}
		if category == spawnMonster && tx.Light(pos) > 0 {
			continue
		}
		if !naturalSpawnPositionValid(tx, pos, category, entry.ID) {
			continue
		}
		group := &world.EntitySpawnGroup{}
		groupSize := entry.MinCount + int(random.NextInt(int32(entry.MaxCount-entry.MinCount+1)))
		for range groupSize {
			offset := cube.Pos{pos[0] + int(random.NextInt(5)) - 2, pos[1], pos[2] + int(random.NextInt(5)) - 2}
			if !naturalSpawnPositionValid(tx, offset, category, entry.ID) {
				continue
			}
			spawnRandom := random.NextLong()
			spawn := w.EntityRegistry().Config().SpawnMob
			if spawn == nil {
				panic("natural spawning requires EntityRegistryConfig.SpawnMob")
			}
			spawnPosition := mgl64.Vec3{float64(offset[0]) + .5, float64(offset[1]), float64(offset[2]) + .5}
			handle := spawn(entry.ID, world.EntitySpawnOpts{
				Position:    spawnPosition,
				SpawnReason: "natural", SpawnSeed: spawnRandom, SpawnGroup: group,
			})
			if handle == nil {
				panic("natural biome spawn table references missing entity factory: " + entry.ID)
			}
			entityBox := handle.Type().BBox(nil).Translate(spawnPosition)
			if !naturalCollisionFree(tx, entityBox) {
				continue
			}
			tx.AddEntity(handle)
			counts[category]++
			if counts[category] >= maxPerCategory[category] {
				break
			}
		}
	}
	phantomSpawnTick(tx, players, &random)
	naturalDespawn(tx)
}

func phantomSpawnTick(tx *world.Tx, players []world.Entity, random *java26.Random) {
	w := tx.World()
	if w.Dimension() != world.Overworld || w.Difficulty() == world.DifficultyPeaceful || w.Time()%24000 < 13000 || w.Time()%24000 > 23000 {
		return
	}
	if tx.CurrentTick()%1800 != 0 {
		return
	}
	for _, player := range players {
		rest, ok := player.(interface{ TimeSinceRest() int32 })
		if !ok || rest.TimeSinceRest() <= 0 {
			continue
		}
		pos := cube.PosFromVec3(player.Position())
		if tx.SkyLight(pos) < 15 {
			continue
		}
		effectiveDifficulty := 2
		if w.Difficulty() == world.DifficultyEasy {
			effectiveDifficulty = 1
		} else if w.Difficulty() == world.DifficultyHard {
			effectiveDifficulty = 3
		}
		restTicks := int64(rest.TimeSinceRest())
		if restTicks < 72000 {
			continue
		}
		if random.NextInt(int32(restTicks)) < 72000 || random.NextFloat()*3 >= float32(effectiveDifficulty) {
			continue
		}
		spawnPos := cube.Pos{
			pos[0] - 10 + int(random.NextInt(21)),
			pos[1] + 20 + int(random.NextInt(15)),
			pos[2] - 10 + int(random.NextInt(21)),
		}
		if !naturalSpawnPositionValid(tx, spawnPos, spawnMonster, "minecraft:phantom") {
			continue
		}
		group := &world.EntitySpawnGroup{}
		spawn := w.EntityRegistry().Config().SpawnMob
		if spawn == nil {
			panic("phantom spawning requires EntityRegistryConfig.SpawnMob")
		}
		for range 1 + random.NextInt(int32(effectiveDifficulty+1)) {
			handle := spawn("minecraft:phantom", world.EntitySpawnOpts{
				Position:    mgl64.Vec3{float64(spawnPos[0]) + .5, float64(spawnPos[1]), float64(spawnPos[2]) + .5},
				SpawnReason: "natural", SpawnSeed: random.NextLong(), SpawnGroup: group,
			})
			if handle == nil {
				panic("phantom spawner references missing entity factory")
			}
			tx.AddEntity(handle)
		}
	}
}

func chooseNaturalEntry(random *java26.Random, rows []world.NaturalSpawnEntry, count, caps []int) *world.NaturalSpawnEntry {
	total := 0
	for i := range rows {
		entry := &rows[i]
		category := naturalSpawnCategoryFor(entry.Category)
		if entry.Weight > 0 && count[category] < caps[category] {
			total += entry.Weight
		}
	}
	if total == 0 {
		return nil
	}
	chosen := int(random.NextInt(int32(total)))
	for i := range rows {
		entry := &rows[i]
		category := naturalSpawnCategoryFor(entry.Category)
		if entry.Weight <= 0 || count[category] >= caps[category] {
			continue
		}
		chosen -= entry.Weight
		if chosen < 0 {
			return entry
		}
	}
	return nil
}

func naturalSpawnCategoryFor(category string) naturalSpawnCategory {
	switch category {
	case "creature":
		return spawnCreature
	case "monster":
		return spawnMonster
	case "ambient":
		return spawnAmbient
	case "water_creature":
		return spawnWaterCreature
	case "water_ambient":
		return spawnWaterAmbient
	case "underground_water_creature":
		return spawnUndergroundWaterCreature
	case "axolotls":
		return spawnAxolotls
	default:
		return spawnMisc
	}
}
func isNaturalWaterCategory(category naturalSpawnCategory) bool {
	return category == spawnWaterCreature || category == spawnWaterAmbient || category == spawnUndergroundWaterCreature || category == spawnAxolotls
}

func naturalCategory(id string) (naturalSpawnCategory, bool) {
	switch id {
	case "minecraft:bat":
		return spawnAmbient, true
	case "minecraft:cod", "minecraft:salmon", "minecraft:tropical_fish", "minecraft:pufferfish", "minecraft:dolphin", "minecraft:nautilus":
		return spawnWaterCreature, true
	case "minecraft:drowned", "minecraft:guardian", "minecraft:elder_guardian", "minecraft:zombie_nautilus":
		return spawnMonster, true
	case "minecraft:glow_squid", "minecraft:squid":
		return spawnWaterAmbient, true
	case "minecraft:axolotl", "minecraft:tadpole":
		return spawnAxolotls, true
	case "minecraft:chicken", "minecraft:cow", "minecraft:wolf", "minecraft:sheep", "minecraft:pig", "minecraft:horse", "minecraft:donkey", "minecraft:goat", "minecraft:rabbit", "minecraft:fox", "minecraft:cat", "minecraft:parrot", "minecraft:llama", "minecraft:panda", "minecraft:polar_bear", "minecraft:camel", "minecraft:armadillo", "minecraft:frog", "minecraft:turtle", "minecraft:bee", "minecraft:mooshroom", "minecraft:sniffer":
		return spawnCreature, true
	case "minecraft:strider":
		return spawnCreature, true
	default:
		if strings.HasPrefix(id, "minecraft:") {
			return spawnMonster, true
		}
		return 0, false
	}
}

func naturalSpawnPositionValid(tx *world.Tx, pos cube.Pos, category naturalSpawnCategory, id string) bool {
	if pos[1] < tx.Range()[0] || pos[1]+1 >= tx.Range()[1] || !tx.ChunkLoaded(world.ChunkPos{int32(pos[0] >> 4), int32(pos[2] >> 4)}) {
		return false
	}
	block, ok := tx.BlockLoaded(pos)
	if !ok || len(block.Model().BBox(pos, tx)) != 0 {
		return false
	}
	liquid, inLiquid := tx.Liquid(pos)
	if isNaturalWaterCategory(category) {
		return inLiquid && liquid.LiquidType() == "water"
	}
	if id == "minecraft:strider" {
		return inLiquid && liquid.LiquidType() == "lava"
	}
	if inLiquid {
		return false
	}
	switch id {
	case "minecraft:evoker", "minecraft:fox", "minecraft:illusioner", "minecraft:panda", "minecraft:phantom",
		"minecraft:shulker", "minecraft:trader_llama", "minecraft:vex", "minecraft:vindicator", "minecraft:warden":
		return true
	}
	floor, ok := tx.BlockLoaded(pos.Side(cube.FaceDown))
	return ok && len(floor.Model().BBox(pos.Side(cube.FaceDown), tx)) != 0
}

func naturalCollisionFree(tx *world.Tx, box cube.BBox) bool {
	for x := int(math.Floor(box.Min()[0])); x <= int(math.Ceil(box.Max()[0])); x++ {
		for y := int(math.Floor(box.Min()[1])); y <= int(math.Ceil(box.Max()[1])); y++ {
			for z := int(math.Floor(box.Min()[2])); z <= int(math.Ceil(box.Max()[2])); z++ {
				pos := cube.Pos{x, y, z}
				block, ok := tx.BlockLoaded(pos)
				if !ok {
					return false
				}
				for _, blockBox := range block.Model().BBox(pos, tx) {
					if box.IntersectsWith(blockBox) {
						return false
					}
				}
			}
		}
	}
	for entity := range tx.EntitiesWithin(box) {
		if box.IntersectsWith(entity.H().Type().BBox(entity).Translate(entity.Position())) {
			return false
		}
	}
	return true
}

func naturalDespawn(tx *world.Tx) {
	players := make([]world.Entity, 0, 4)
	for p := range tx.Players() {
		mode, ok := p.(interface{ GameMode() world.GameMode })
		if !ok || mode.GameMode() != world.GameModeSpectator {
			players = append(players, p)
		}
	}
	random := java26.NewLegacyRandom(tx.World().Seed() ^ int64(uint64(tx.CurrentTick())*0xd1b54a32d192ed03))
	for entity := range tx.Entities() {
		if entity.H().SpawnReason() != "natural" {
			continue
		}
		if persistent, ok := entity.(interface{ Tamed() bool }); ok && persistent.Tamed() {
			continue
		}
		category, ok := naturalCategory(entity.H().Type().EncodeEntity())
		if !ok {
			continue
		}
		if tx.World().Difficulty() == world.DifficultyPeaceful && category == spawnMonster {
			tx.RemoveEntity(entity)
			continue
		}
		if len(players) == 0 {
			continue
		}
		nearest := math.MaxFloat64
		for _, player := range players {
			distanceSq := player.Position().Sub(entity.Position()).LenSqr()
			if distanceSq < nearest {
				nearest = distanceSq
			}
		}
		if nearest > 128*128 {
			tx.RemoveEntity(entity)
			continue
		}
		if age, ok := entity.(interface{ Age() time.Duration }); ok && age.Age() > 30*time.Second && nearest > 32*32 && random.NextInt(800) == 0 {
			tx.RemoveEntity(entity)
		}
	}
}
