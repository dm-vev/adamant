package entity

import (
	"math"
	"sync"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// This file ports the custom spawners of ServerLevel that place mobs in
// villages: CatSpawner and VillageSiege.

// FinalizeSpawn ports Cat.finalizeSpawn: a cat in a swamp hut is all black,
// and otherwise picks any variant, all black too under a full moon, and a
// random sound variant.
func (c *Cat) FinalizeSpawn(_ string, r *vanilla.Random) {
	if r == nil || c.tx == nil {
		return
	}
	b := c.b()
	pos := cube.PosFromVec3(c.Position())
	inHut := false
	for _, a := range c.tx.World().SpawnStructures(world.ChunkPos{int32(pos[0] >> 4), int32(pos[2] >> 4)}) {
		if a.ID == "minecraft:swamp_hut" && a.Contains(pos, true) {
			inHut = true
		}
	}
	allBlack := len(catVariantNames) - 1
	switch {
	case inHut:
		// The structure selector of all_black has priority 1.
		b.variant = allBlack
	case c.tx.World().MoonBrightness() >= .9:
		b.variant = int(r.NextInt(int32(len(catVariantNames))))
	default:
		b.variant = int(r.NextInt(int32(allBlack)))
	}
	b.soundVariant = [...]string{"classic", "royal"}[r.NextInt(2)]
}

// customSpawnerState is the state CatSpawner and VillageSiege keep for a
// world.
type customSpawnerState struct {
	catNextTick int

	siegeTonight, siegeSetUp  bool
	zombiesToSpawn, nextSpawn int
	spawnX, spawnY, spawnZ    int
	siegeRolledAt             int64
}

var customSpawnerStates sync.Map

func customSpawnersOf(w *world.World) *customSpawnerState {
	if s, ok := customSpawnerStates.Load(w); ok {
		return s.(*customSpawnerState)
	}
	actual, _ := customSpawnerStates.LoadOrStore(w, &customSpawnerState{siegeRolledAt: -1})
	return actual.(*customSpawnerState)
}

// catSpawnTick ports CatSpawner.tick: once a minute a cat may spawn near a
// random player, in a village with more than four occupied beds and fewer
// than five cats, or in a swamp hut without a cat.
func catSpawnTick(tx *world.Tx, r *vanilla.Random) {
	s := customSpawnersOf(tx.World())
	if s.catNextTick--; s.catNextTick > 0 {
		return
	}
	s.catNextTick = 1200
	var players []world.Entity
	for p := range tx.Players() {
		if entityAlive(p) {
			players = append(players, p)
		}
	}
	if len(players) == 0 {
		return
	}
	player := players[r.NextInt(int32(len(players)))]
	x := (8 + int(r.NextInt(24))) * signOf(r.NextBoolean())
	z := (8 + int(r.NextInt(24))) * signOf(r.NextBoolean())
	pos := cube.PosFromVec3(player.Position()).Add(cube.Pos{x, 0, z})
	for cx := (pos[0] - 10) >> 4; cx <= (pos[0]+10)>>4; cx++ {
		for cz := (pos[2] - 10) >> 4; cz <= (pos[2]+10)>>4; cz++ {
			if !tx.ChunkLoaded(world.ChunkPos{int32(cx), int32(cz)}) {
				return
			}
		}
	}
	if !spawnPositionOk(tx, "minecraft:cat", pos) {
		return
	}
	if tx.VillageDistance(pos) <= 2 {
		homes := 0
		for p, poi := range tx.POIsInRange(pos, 48) {
			if poi.Kind == world.Home && len(poi.Owners) > 0 && p.Vec3().Sub(pos.Vec3()).LenSqr() <= 48*48 {
				homes++
			}
		}
		if homes > 4 && catsWithin(tx, pos, 48) < 5 {
			spawnCat(tx, pos, false, r)
		}
		return
	}
	for _, a := range tx.World().SpawnStructures(world.ChunkPos{int32(pos[0] >> 4), int32(pos[2] >> 4)}) {
		if a.ID == "minecraft:swamp_hut" && a.Contains(pos, true) {
			if catsWithin(tx, pos, 16) == 0 {
				spawnCat(tx, pos, true, r)
			}
			return
		}
	}
}

func signOf(negative bool) int {
	if negative {
		return -1
	}
	return 1
}

// catsWithin counts the cats in the box radius blocks around pos and eight
// up and down.
func catsWithin(tx *world.Tx, pos cube.Pos, radius float64) int {
	box := cube.Box(0, 0, 0, 1, 1, 1).Translate(pos.Vec3()).GrowVec3(mgl64.Vec3{radius, 8, radius})
	n := 0
	for e := range tx.EntitiesWithin(box) {
		if _, ok := e.(*Cat); ok {
			n++
		}
	}
	return n
}

// spawnCat ports CatSpawner.spawnCat.
func spawnCat(tx *world.Tx, pos cube.Pos, persistent bool, r *vanilla.Random) {
	opts := world.EntitySpawnOpts{Position: pos.Vec3Middle(), SpawnReason: "natural", SpawnSeed: r.NextLong()}
	cat := tx.AddEntity(opts.New(CatType, CatBehaviourConfig{}))
	if persistent {
		if c, ok := cat.(*Cat); ok {
			c.SetPersistenceRequired(true)
		}
	}
}

// villageSiegeTick ports VillageSiege.tick: one night in ten, at midnight, up
// to 20 zombies spawn one by one at the edge of the village a player is in.
func villageSiegeTick(tx *world.Tx, enemies bool, r *vanilla.Random) {
	w := tx.World()
	s := customSpawnersOf(w)
	if brightOutside(tx) || !enemies || w.Dimension() != world.Overworld {
		s.siegeTonight, s.siegeSetUp = false, false
		return
	}
	if day := int64(w.Time()); day%24000 == 18000 && s.siegeRolledAt != day {
		// ClockTimeMarkers.ROLL_VILLAGE_SIEGE.
		s.siegeRolledAt = day
		s.siegeTonight = r.NextInt(10) == 0
	}
	if !s.siegeTonight {
		return
	}
	if !s.siegeSetUp {
		if !s.setUpSiege(tx, r) {
			return
		}
		s.siegeSetUp = true
	}
	if s.nextSpawn > 0 {
		s.nextSpawn--
		return
	}
	s.nextSpawn = 2
	if s.zombiesToSpawn <= 0 {
		s.siegeTonight = false
		return
	}
	s.zombiesToSpawn--
	if pos, ok := s.siegeSpawnPos(tx, cube.Pos{s.spawnX, s.spawnY, s.spawnZ}, r); ok {
		opts := world.EntitySpawnOpts{Position: pos, Rotation: cube.Rotation{float64(r.NextFloat() * 360), 0}, SpawnReason: "event", SpawnSeed: r.NextLong()}
		tx.AddEntity(NewZombie(opts))
	}
}

// setUpSiege ports VillageSiege.tryToSetupSiege.
func (s *customSpawnerState) setUpSiege(tx *world.Tx, r *vanilla.Random) bool {
	for p := range tx.Players() {
		if spectator(p) {
			continue
		}
		centre := cube.PosFromVec3(p.Position())
		if !tx.IsVillage(centre) || biomeHasTag(tx, centre, "without_zombie_sieges") {
			continue
		}
		for range 10 {
			angle := float64(r.NextFloat() * (math.Pi * 2))
			s.spawnX = centre[0] + int(math.Floor(float64(float32(math.Cos(angle))*32)))
			s.spawnY = centre[1]
			s.spawnZ = centre[2] + int(math.Floor(float64(float32(math.Sin(angle))*32)))
			if _, ok := s.siegeSpawnPos(tx, cube.Pos{s.spawnX, s.spawnY, s.spawnZ}, r); ok {
				s.nextSpawn, s.zombiesToSpawn = 0, 20
				break
			}
		}
		return true
	}
	return false
}

// siegeSpawnPos ports VillageSiege.findRandomSpawnPos: a surface spot in the
// village near pos dark enough for a zombie.
func (s *customSpawnerState) siegeSpawnPos(tx *world.Tx, pos cube.Pos, r *vanilla.Random) (mgl64.Vec3, bool) {
	for range 10 {
		x, z := pos[0]+int(r.NextInt(16))-8, pos[2]+int(r.NextInt(16))-8
		at := cube.Pos{x, tx.HighestBlock(x, z) + 1, z}
		if tx.IsVillage(at) && checkMonsterSpawnRules(tx, "minecraft:zombie", at, r) {
			return at.Vec3Middle(), true
		}
	}
	return mgl64.Vec3{}, false
}
