package entity

import (
	"sync"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
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
	// WanderingTraderSpawner owns its own random and ticks without players.
	traderRandom := vanilla.NewLegacyRandom(w.Seed() ^ int64(uint64(tx.CurrentTick())*0x632be59bd9b4e019))
	wanderingTraderSpawnTick(tx, &traderRandom)
	players := make([]world.Entity, 0, 4)
	for p := range tx.Players() {
		mode, ok := p.(interface{ GameMode() world.GameMode })
		if !ok || mode.GameMode() != world.GameModeSpectator {
			players = append(players, p)
		}
	}
	// Java skips every spawning category and custom spawner without
	// spawn_mobs, but still despawns.
	if len(players) == 0 || !w.SpawnMobsEnabled() {
		return
	}
	random := levelRandom(w)
	spawnNaturally(tx, players, random)
	// ServerLevel.tickCustomSpawners after the chunks spawned their mobs.
	phantomSpawnTick(tx, players, random)
	patrolSpawnTick(tx, random)
}

// levelRandoms are the Level.random streams of the worlds mobs spawn in.
var levelRandoms sync.Map

// levelRandom returns the random stream of a world that mob spawning draws
// from, as Level.random.
func levelRandom(w *world.World) *vanilla.Random {
	if r, ok := levelRandoms.Load(w); ok {
		return r.(*vanilla.Random)
	}
	r := vanilla.NewLegacyRandom(w.Seed() ^ 0x5deece66d)
	actual, _ := levelRandoms.LoadOrStore(w, &r)
	return actual.(*vanilla.Random)
}

// phantomNextTicks are the PhantomSpawner.nextTick of the worlds.
var phantomNextTicks sync.Map

// phantomSpawnTick ports PhantomSpawner.tick: once a minute or two, at night
// or in a dimension without sky light, a player who has not slept in three
// days and sees the sky may have phantoms spawn above them.
func phantomSpawnTick(tx *world.Tx, players []world.Entity, random *vanilla.Random) {
	w := tx.World()
	if w.Difficulty() == world.DifficultyPeaceful || !w.MonsterSpawningEnabled() || !w.GameRuleBool("spawn_phantoms") {
		return
	}
	next := 0
	if v, ok := phantomNextTicks.Load(w); ok {
		next = v.(int)
	}
	next--
	defer func() { phantomNextTicks.Store(w, next) }()
	if next > 0 {
		return
	}
	next += (60 + int(random.NextInt(60))) * 20
	skyLight := w.Dimension() == world.Overworld
	if tx.SkyDarken() < 5 && skyLight {
		return
	}
	spawn := w.EntityRegistry().Config().SpawnMob
	if spawn == nil {
		return
	}
	for _, player := range players {
		pos := cube.PosFromVec3(player.Position())
		if skyLight && (pos[1] < seaLevel(tx) || !canSeeSky(tx, pos)) {
			continue
		}
		difficulty := tx.LocalDifficulty(pos)
		if !(difficulty.Effective > random.NextFloat()*3) {
			continue
		}
		rest := int32(1)
		if r, ok := player.(interface{ TimeSinceRest() int32 }); ok {
			rest = max(r.TimeSinceRest(), 1)
		}
		if random.NextInt(rest) < 72000 {
			continue
		}
		spawnPos := cube.Pos{pos[0], pos[1] + 20 + int(random.NextInt(15)), pos[2]}
		spawnPos[0] += -10 + int(random.NextInt(21))
		spawnPos[2] += -10 + int(random.NextInt(21))
		if !validEmptySpawnBlock(tx, spawnPos, "minecraft:phantom") {
			continue
		}
		group := &world.EntitySpawnGroup{}
		for range 1 + random.NextInt(int32(difficultyID(w.Difficulty())+1)) {
			handle := spawn("minecraft:phantom", world.EntitySpawnOpts{
				Position:    mgl64.Vec3{float64(spawnPos[0]) + .5, float64(spawnPos[1]), float64(spawnPos[2]) + .5},
				SpawnReason: "natural", SpawnSeed: random.NextLong(), SpawnGroup: group,
			})
			if handle != nil {
				tx.AddEntity(handle)
			}
		}
	}
}

// difficultyID is Difficulty.getId.
func difficultyID(d world.Difficulty) int {
	switch d {
	case world.DifficultyEasy:
		return 1
	case world.DifficultyNormal:
		return 2
	case world.DifficultyHard:
		return 3
	}
	return 0
}
