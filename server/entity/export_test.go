package entity

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
)

// SpawnerRandom returns the random stream of seed the custom spawners draw
// from.
func SpawnerRandom(seed int64) *vanilla.Random {
	r := vanilla.NewLegacyRandom(seed)
	return &r
}

// PhantomSpawnTick runs the phantom spawner at once, over every player.
func PhantomSpawnTick(tx *world.Tx, random *vanilla.Random) {
	phantomNextTicks.Store(tx.World(), 0)
	var players []world.Entity
	for p := range tx.Players() {
		players = append(players, p)
	}
	phantomSpawnTick(tx, players, random)
}

// PatrolSpawnTick runs the patrol spawner at once.
func PatrolSpawnTick(tx *world.Tx, random *vanilla.Random) {
	v, _ := patrolSpawners.LoadOrStore(tx.World(), new(int))
	*v.(*int) = 0
	patrolSpawnTick(tx, random)
}

// CatSpawnTick runs the cat spawner at once.
func CatSpawnTick(tx *world.Tx, random *vanilla.Random) {
	customSpawnersOf(tx.World()).catNextTick = 0
	catSpawnTick(tx, random)
}

// VillageSiegeTick starts a siege of the village a player is in and runs the
// spawner once.
func VillageSiegeTick(tx *world.Tx, random *vanilla.Random) {
	s := customSpawnersOf(tx.World())
	s.siegeTonight, s.siegeSetUp, s.nextSpawn = true, false, 0
	villageSiegeTick(tx, true, random)
}

// WanderingTraderSpawnTick runs the wandering trader spawner at once, with
// a day passed and a spawn chance of 75%.
func WanderingTraderSpawnTick(tx *world.Tx, random *vanilla.Random) {
	v, _ := wanderingTraderSpawners.LoadOrStore(tx.World(), &wanderingTraderSpawner{})
	s := v.(*wanderingTraderSpawner)
	s.tickDelay, s.data = 1, &wanderingTraderData{SpawnDelay: 1200, SpawnChance: 75}
	wanderingTraderSpawnTick(tx, random)
}

// ForceVillageSiegeTonight makes the coming night of w a siege night, as one
// in ten VillageSiege rolls.
func ForceVillageSiegeTonight(w *world.World) {
	s := customSpawnersOf(w)
	s.siegeTonight, s.siegeRolledAt = true, int64(w.Time())-int64(w.Time())%24000+18000
}

// OminousBannerStack returns the ominous banner raiders carry.
var OminousBannerStack = ominousBannerStack

// DragonFlame returns the cloud the flaming phase of a dragon breathed.
func DragonFlame(d *EnderDragon) *world.EntityHandle { return d.behaviour().ph.flame }

// DragonFlameCount returns and SetDragonFlameCount sets how often the dragon
// breathed since it landed.
func DragonFlameCount(d *EnderDragon) int       { return d.behaviour().ph.flameCount }
func SetDragonFlameCount(d *EnderDragon, n int) { d.behaviour().ph.flameCount = n }

// DragonInWall reports if blocks the dragon cannot break slow it.
func DragonInWall(d *EnderDragon) bool { return d.behaviour().inWall }

// DragonNodes returns the nodes the dragon flies between.
func DragonNodes(tx *world.Tx, d *EnderDragon) [24]cube.Pos {
	d.behaviour().ensureDragonNodes(tx)
	return d.behaviour().nodes
}

// DragonClosestNode returns the node closest to the dragon.
func DragonClosestNode(tx *world.Tx, d *EnderDragon) int { return d.behaviour().closestNode(tx, d.Ent) }

// DragonFindPath returns the nodes of the path the dragon finds.
func DragonFindPath(tx *world.Tx, d *EnderDragon, start, end int, final *cube.Pos) ([]cube.Pos, bool) {
	p := d.behaviour().findPath(tx, start, end, final)
	return p.nodes[:p.n], p.valid
}

// NewDragonBreathCloud returns a cloud of the dragon's breath.
var NewDragonBreathCloud = newDragonBreathCloud

// GhastlingWalkTarget returns the entity the brain of a ghastling flies it
// to, nil if it flies to a block, and the speed modifier it flies at. It
// reports false if the ghastling flies nowhere.
func GhastlingWalkTarget(tx *world.Tx, g *HappyGhast) (world.Entity, float64, bool) {
	v, ok := g.state().br.get(memWalkTarget)
	if !ok {
		return nil, 0, false
	}
	wt := v.(walkTarget)
	if t, ok := wt.target.(*entityTracker); ok {
		return t.entity(tx), wt.speed, true
	}
	return nil, wt.speed, true
}
