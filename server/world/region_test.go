package world_test

import (
	"os"
	"runtime/pprof"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/go-gl/mathgl/mgl64"
)

// TestRegionGroups checks columns join a region when their cells touch, and
// stay apart otherwise.
func TestRegionGroups(t *testing.T) {
	sorted := func(groups [][]world.ChunkPos) [][]world.ChunkPos {
		for _, g := range groups {
			slices.SortFunc(g, func(a, b world.ChunkPos) int {
				if a[0] != b[0] {
					return int(a[0] - b[0])
				}
				return int(a[1] - b[1])
			})
		}
		return groups
	}
	cases := []struct {
		name    string
		columns []world.ChunkPos
		want    int
	}{
		{"one cell", []world.ChunkPos{{0, 0}, {15, 15}}, 1},
		{"touching cells", []world.ChunkPos{{0, 0}, {16, 0}}, 1},
		{"diagonal cells", []world.ChunkPos{{15, 15}, {16, 16}}, 1},
		{"negative cells", []world.ChunkPos{{-1, -1}, {0, 0}}, 1},
		{"cell apart", []world.ChunkPos{{0, 0}, {32, 0}}, 2},
		{"cells apart", []world.ChunkPos{{0, 0}, {64, 64}, {-64, 0}}, 3},
		{"cells beside, as entities 300 blocks apart", []world.ChunkPos{{0, 0}, {19, 0}}, 1},
		{"chain", []world.ChunkPos{{0, 0}, {16, 0}, {32, 0}, {48, 0}}, 1},
	}
	for _, c := range cases {
		groups := sorted(world.RegionGroups(c.columns))
		if len(groups) != c.want {
			t.Errorf("%s: %d regions %v, want %d", c.name, len(groups), groups, c.want)
		}
		seen := 0
		for _, g := range groups {
			seen += len(g)
		}
		if seen != len(c.columns) {
			t.Errorf("%s: regions hold %d columns, want %d", c.name, seen, len(c.columns))
		}
	}
}

// regionCentres are the centres of the areas TestRegionsTickWithoutRaces
// fills, far enough apart for each to be a region.
var regionCentres = []cube.Pos{{0, 0, 0}, {1024, 0, 0}, {0, 0, 1024}, {1024, 0, 1024}, {-1024, 0, -512}}

// TestRegionsTickWithoutRaces ticks areas far apart full of mobs, players,
// projectiles, explosions, villages and farms in regions at once, for the
// race detector to find any state two regions share without a lock, and
// fails if a tick does not finish in time, as a deadlock would.
func TestRegionsTickWithoutRaces(t *testing.T) {
	tickAreasWithoutRaces(t, regionCentres, 1)
}

// TestCrowdedRegionsTickWithoutRaces ticks such areas side by side on three
// by three cells, which make one region ticking its cells in phases, the
// areas straddling the borders of cells, for the race detector to find any
// state the cells of a phase share without a lock.
func TestCrowdedRegionsTickWithoutRaces(t *testing.T) {
	var centres []cube.Pos
	for x := range 3 {
		for z := range 3 {
			centres = append(centres, cube.Pos{x * 256, 0, z * 256})
		}
	}
	tickAreasWithoutRaces(t, centres, 4)
}

// tickAreasWithoutRaces fills areas around the centres passed and ticks
// them in regions, wanting the phases passed at least a tick.
func tickAreasWithoutRaces(t *testing.T, regionCentres []cube.Pos, phasesPerTick int) {
	ticks := 300
	if testing.Short() {
		ticks = 80
	}
	// A world ticking on its own goroutine, as delayed callbacks such as a
	// player respawning run in transactions of their own. AdvanceTick adds
	// ticks to the ones it runs itself.
	w := world.Config{Regions: true, RandomTickSpeed: 3, Entities: entity.DefaultRegistry, Generator: regionTestGenerator{}, Provider: world.NopProvider{}}.New()
	defer w.Close()
	w.SetDifficulty(world.DifficultyHard)

	viewers := make([]*countingViewer, len(regionCentres))
	var players []*world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		for i, c := range regionCentres {
			// The loaders make the columns around the centres tick actively.
			viewers[i] = &countingViewer{}
			l := world.NewLoader(4, w, viewers[i])
			l.Move(tx, c.Vec3Centre())
			l.Load(tx, 128)
			players = append(players, fillRegionArea(tx, c))
		}
		// An arrow in the first area shot by the player of the second: its
		// owner is in another region while it flies.
		c := regionCentres[0]
		owner, _ := players[1].Entity(tx)
		tx.AddEntity(entity.NewArrow(world.EntitySpawnOpts{Position: mgl64.Vec3{float64(c[0]), 70, float64(c[2])}, Velocity: mgl64.Vec3{0.5, 0.2, 0.3}}, owner))
		// Arrows flying out of the generated chunks move into chunks not
		// loaded yet, which the owner loads after the phase.
		for i, c := range regionCentres {
			dir := float64(1 - 2*(i%2))
			shooter, _ := players[i].Entity(tx)
			tx.AddEntity(entity.NewArrow(world.EntitySpawnOpts{Position: mgl64.Vec3{float64(c[0]) + dir*60, 75, float64(c[2])}, Velocity: mgl64.Vec3{dir * 3, 0.1, 0}}, shooter))
		}
	})

	done := make(chan struct{})
	var ticked atomic.Int64
	go func() {
		defer close(done)
		for range ticks {
			w.AdvanceTick()
			ticked.Add(1)
		}
	}()
	timeout := 5 * time.Minute
	select {
	case <-done:
	case <-time.After(timeout):
		_ = pprof.Lookup("goroutine").WriteTo(os.Stderr, 2)
		t.Fatalf("ticks stopped after %d of %d: deadlock", ticked.Load(), ticks)
	}

	if phases := w.RegionPhases(); phases < int64(ticks*phasesPerTick) {
		t.Errorf("%d region phases in %d ticks, want %d a tick at least", phases, ticks, phasesPerTick)
	}
	for i, v := range viewers {
		if v.calls.Load() == 0 {
			t.Errorf("viewer of area %d was never shown anything", i)
		}
	}
	<-w.Exec(func(tx *world.Tx) {
		alive := 0
		for range tx.Entities() {
			alive++
		}
		if alive == 0 {
			t.Error("no entity left in the world")
		}
		t.Logf("%d ticks, %d region phases, %d entities left", ticks, w.RegionPhases(), alive)
		for i, h := range players {
			if _, ok := h.Entity(tx); !ok {
				t.Errorf("player of area %d left the world", i)
			}
		}
	})
}

// fillRegionArea builds a village with farms around c and fills it with
// mobs and a player, whose handle it returns.
func fillRegionArea(tx *world.Tx, c cube.Pos) *world.EntityHandle {
	at := func(x, y, z int) cube.Pos { return cube.Pos{c[0] + x, y, c[2] + z} }
	for x := -24; x < 24; x++ {
		for z := -24; z < 24; z++ {
			switch {
			case x > 10 && z > 10 && x < 16 && z < 16:
				tx.SetBlock(at(x, 63, z), block.Water{Depth: 8, Still: true}, nil)
			case x >= -20 && x < -14 && z >= -20 && z < -14:
				tx.SetBlock(at(x, 63, z), block.Farmland{Hydration: 7}, nil)
				wheat := block.WheatSeeds{}
				wheat.Growth = 7
				tx.SetBlock(at(x, 64, z), wheat, nil)
			}
			if x == -10 && z%3 == 0 {
				tx.SetBlock(at(x, 64, z), block.WoodFence{Wood: block.OakWood()}, nil)
			}
		}
	}
	for i := 0; i < 4; i++ {
		tx.SetBlock(at(8+i*2, 64, -8), block.Bed{Facing: cube.North}, nil)
		tx.SetBlock(at(8+i*2, 64, -9), block.Bed{Facing: cube.North, Head: true}, nil)
	}
	tx.SetBlock(at(0, 64, -12), block.Composter{}, nil)
	tx.SetBlock(at(2, 64, -12), block.Lectern{}, nil)
	tx.SetBlock(at(4, 64, -12), block.Bell{}, nil)
	tx.SetBlock(at(-4, 64, 0), block.TNT{}, nil)
	tx.SetBlock(at(6, 64, 6), block.HayBale{}, nil)

	spawn := func(x, z int) world.EntitySpawnOpts {
		return world.EntitySpawnOpts{Position: at(x, 64, z).Vec3Middle()}
	}
	conf := player.Config{Position: c.Add(cube.Pos{0, 64, 0}).Vec3Middle(), GameMode: world.GameModeSurvival}
	p := tx.AddEntity(world.EntitySpawnOpts{Position: conf.Position}.New(player.Type, conf))
	for i := 0; i < 3; i++ {
		tx.AddEntity(entity.NewZombie(spawn(-15+i*5, -15)))
		tx.AddEntity(entity.NewSkeleton(spawn(-15+i*5, -10)))
		tx.AddEntity(entity.NewVillager(spawn(-15+i*5, 5)))
		tx.AddEntity(entity.NewSheep(spawn(-15+i*5, 10)))
		tx.AddEntity(entity.NewChicken(spawn(-15+i*5, 15), entity.ChickenBehaviourConfig{}))
		tx.AddEntity(entity.NewCow(spawn(5+i*3, -5)))
		tx.AddEntity(entity.NewCreeper(spawn(5+i*3, 0)))
		tx.AddEntity(entity.NewSpider(spawn(-5, -5+i*3)))
		tx.AddEntity(entity.NewEnderman(spawn(-8+i*4, 18)))
		tx.AddEntity(entity.NewPillager(spawn(18, -18+i*4)))
		tx.AddEntity(entity.NewFox(spawn(-18, 0+i*3)))
		tx.AddEntity(entity.NewBee(spawn(0, 20+i)))
		tx.AddEntity(entity.NewWolf(spawn(3+i, 3), entity.WolfBehaviourConfig{Tamed: true, OwnerUUID: p.H().UUID().String()}))
		tx.AddEntity(entity.NewItem(spawn(i*2, 2), item.NewStack(item.Wheat{}, 3)))
		tx.AddEntity(entity.NewItem(spawn(i*2, 3), item.NewStack(item.Bread{}, 2)))
		tx.AddEntity(entity.NewExperienceOrb(spawn(-i, 1), 5))
	}
	tx.AddEntity(entity.NewIronGolem(spawn(0, 8)))
	tx.AddEntity(entity.NewWitch(spawn(-12, -4)))
	tx.AddEntity(entity.NewSlime(spawn(10, 10)))
	tx.AddEntity(entity.NewTNT(world.EntitySpawnOpts{Position: at(-6, 64, 0).Vec3Middle()}, 3*time.Second, nil))
	return p.H()
}

// regionTestGenerator generates flat grass over dirt and stone.
type regionTestGenerator struct{}

func (regionTestGenerator) GenerateChunk(_ world.ChunkPos, c *chunk.Chunk, _ world.GenerationContext) world.GenerationResult {
	stone, _ := chunk.StateToRuntimeID("minecraft:stone", nil)
	dirt, _ := chunk.StateToRuntimeID("minecraft:dirt", map[string]any{"dirt_type": "normal"})
	grass, _ := chunk.StateToRuntimeID("minecraft:grass_block", nil)
	for x := range uint8(16) {
		for z := range uint8(16) {
			for y := int16(c.Range().Min()); y < 60; y++ {
				c.SetBlock(x, y, z, 0, stone)
			}
			for y := int16(60); y < 63; y++ {
				c.SetBlock(x, y, z, 0, dirt)
			}
			c.SetBlock(x, 63, z, 0, grass)
		}
	}
	return world.GenerationResult{}
}

func (regionTestGenerator) DefaultSpawn(world.Dimension) cube.Pos { return cube.Pos{0, 64, 0} }

// countingViewer counts the calls made to it. Regions may call it at once.
type countingViewer struct {
	world.NopViewer
	calls atomic.Int64
}

func (v *countingViewer) enter() func() {
	v.calls.Add(1)
	return func() {}
}

func (v *countingViewer) ViewEntity(world.Entity)       { defer v.enter()() }
func (v *countingViewer) HideEntity(world.Entity)       { defer v.enter()() }
func (v *countingViewer) ViewEntityState(world.Entity)  { defer v.enter()() }
func (v *countingViewer) ViewEntityItems(world.Entity)  { defer v.enter()() }
func (v *countingViewer) ViewEntityArmour(world.Entity) { defer v.enter()() }
func (v *countingViewer) ViewEntityMovement(world.Entity, mgl64.Vec3, cube.Rotation, bool) {
	defer v.enter()()
}
func (v *countingViewer) ViewEntityVelocity(world.Entity, mgl64.Vec3)    { defer v.enter()() }
func (v *countingViewer) ViewEntityMove(world.Entity, *world.EntityMove) { defer v.enter()() }
func (v *countingViewer) ViewEntityAction(world.Entity, world.EntityAction) {
	defer v.enter()()
}
func (v *countingViewer) ViewEntityAnimation(world.Entity, world.EntityAnimation) {
	defer v.enter()()
}
func (v *countingViewer) ViewParticle(mgl64.Vec3, world.Particle) { defer v.enter()() }
func (v *countingViewer) ViewSound(mgl64.Vec3, world.Sound)       { defer v.enter()() }
func (v *countingViewer) ViewBlockUpdate(cube.Pos, world.Block, int) {
	defer v.enter()()
}
func (v *countingViewer) ViewBlockUpdates(*world.BlockUpdates)        { defer v.enter()() }
func (v *countingViewer) ViewBlockAction(cube.Pos, world.BlockAction) { defer v.enter()() }

// TestRegionMonstersDespawnFarFromPlayersOfOtherRegions checks monsters in
// a region without players leave the world when the only player is in
// another region, as Mob.checkDespawn removes monsters with every player
// further than 128 blocks away, rather than staying as if the world had no
// players.
func TestRegionMonstersDespawnFarFromPlayersOfOtherRegions(t *testing.T) {
	w := world.Config{Synchronous: true, Regions: true, Entities: entity.DefaultRegistry, Generator: regionTestGenerator{}, Provider: world.NopProvider{}}.New()
	defer w.Close()
	w.SetDifficulty(world.DifficultyHard)
	var zombies []*world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		conf := player.Config{Position: mgl64.Vec3{2048.5, 64, 0.5}, GameMode: world.GameModeSurvival}
		tx.AddEntity(world.EntitySpawnOpts{Position: conf.Position}.New(player.Type, conf))
		for i := range 4 {
			zombies = append(zombies, tx.AddEntity(entity.NewZombie(world.EntitySpawnOpts{Position: mgl64.Vec3{float64(i*3) + 0.5, 64, 0.5}})).H())
		}
	})
	w.AdvanceTick()
	if w.RegionPhases() == 0 {
		t.Fatal("no region phase ran")
	}
	<-w.Exec(func(tx *world.Tx) {
		for i, h := range zombies {
			if _, ok := h.Entity(tx); ok {
				t.Errorf("zombie %d stayed with the only player 2048 blocks away", i)
			}
		}
	})
}
