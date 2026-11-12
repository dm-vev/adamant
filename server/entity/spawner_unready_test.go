package entity_test

import (
	"sync"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// gatedGenerator generates flat stone ground at once in the chunks within
// radius of the origin, and holds every other chunk until it is released: a
// generator that the players who fly on have outrun.
type gatedGenerator struct {
	radius  int32
	release chan struct{}
	once    sync.Once
}

func (g *gatedGenerator) GenerateChunk(pos world.ChunkPos, c *chunk.Chunk, _ world.GenerationContext) world.GenerationResult {
	if pos[0] < -g.radius || pos[0] > g.radius || pos[1] < -g.radius || pos[1] > g.radius {
		<-g.release
	}
	stone, _ := chunk.StateToRuntimeID("minecraft:stone", nil)
	for x := range uint8(16) {
		for z := range uint8(16) {
			for y := int16(c.Range().Min()); y < 64; y++ {
				c.SetBlock(x, y, z, 0, stone)
			}
		}
	}
	return world.GenerationResult{}
}

func (*gatedGenerator) DefaultSpawn(world.Dimension) cube.Pos { return cube.Pos{0, 64, 0} }

func (g *gatedGenerator) open() { g.once.Do(func() { close(g.release) }) }

// farAway is where the explorer of the tests stands, in a column that the
// generator did not get to.
var farAway = mgl64.Vec3{336.5, 64, 336.5}

// gatedWorld makes a world whose generator is ahead of nothing beyond radius
// chunks of the origin, with a player at the origin, who stands in a
// generated column, and an explorer far away, whose column is still queued.
// The chunks within radius are loaded.
func gatedWorld(t *testing.T, radius int32, time int) (*world.World, *gatedGenerator) {
	t.Helper()
	g := &gatedGenerator{radius: radius, release: make(chan struct{})}
	w := world.Config{Entities: entity.DefaultRegistry, Generator: g, GeneratorWorkers: 2, Provider: world.NopProvider{}, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { g.open(); _ = w.Close() })
	w.SetDifficulty(world.DifficultyHard)
	w.SetTime(time)
	w.StopTime()
	<-w.Exec(func(tx *world.Tx) {
		for x := -radius; x <= radius; x++ {
			for z := -radius; z <= radius; z++ {
				tx.Block(cube.Pos{int(x) << 4, 64, int(z) << 4})
			}
		}
		tx.AddEntity((world.EntitySpawnOpts{}).New(player.Type, player.Config{Position: mgl64.Vec3{.5, 64, .5}, GameMode: world.GameModeSurvival, TimeSinceRest: 10_000_000}))
		tx.AddEntity((world.EntitySpawnOpts{}).New(player.Type, player.Config{Position: farAway, GameMode: world.GameModeSurvival, TimeSinceRest: 10_000_000}))
	})
	return w, g
}

// mustNotWait runs f on the owner of w and fails the test if it waits for the
// generator, which it holds until then.
func mustNotWait(t *testing.T, w *world.World, g *gatedGenerator, what string, f func(tx *world.Tx)) {
	t.Helper()
	done := w.Exec(f)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Errorf("%s waited on the world owner for a column the generator did not finish", what)
		g.open()
		<-done
	}
}

func TestPhantomSpawnerDoesNotWaitForGeneration(t *testing.T) {
	t.Parallel()
	w, g := gatedWorld(t, 0, 18000)
	mustNotWait(t, w, g, "the phantom spawner", func(tx *world.Tx) {
		for seed := range int64(50) {
			entity.PhantomSpawnTick(tx, entity.SpawnerRandom(seed))
		}
	})
}

func TestPhantomsStillSpawnOverAGeneratedColumn(t *testing.T) {
	t.Parallel()
	w, g := gatedWorld(t, 0, 18000)
	phantoms := 0
	mustNotWait(t, w, g, "the phantom spawner", func(tx *world.Tx) {
		for seed := range int64(300) {
			entity.PhantomSpawnTick(tx, entity.SpawnerRandom(seed))
			if phantoms = countEntities(tx, "minecraft:phantom"); phantoms != 0 {
				return
			}
		}
	})
	if phantoms == 0 {
		t.Fatal("no phantom spawned over a player in a generated column")
	}
}

func TestPatrolSpawnerDoesNotWaitForGeneration(t *testing.T) {
	t.Parallel()
	// The patrol is placed up to 47 blocks away and its members drift a
	// few more: the generated area ends a little beyond.
	w, g := gatedWorld(t, 5, 6000)
	mustNotWait(t, w, g, "the patrol spawner", func(tx *world.Tx) {
		for seed := range int64(400) {
			entity.PatrolSpawnTick(tx, entity.SpawnerRandom(seed))
		}
	})
}

func TestCatSpawnerDoesNotWaitForGeneration(t *testing.T) {
	t.Parallel()
	w, g := gatedWorld(t, 3, 6000)
	mustNotWait(t, w, g, "the cat spawner", func(tx *world.Tx) {
		for seed := range int64(100) {
			entity.CatSpawnTick(tx, entity.SpawnerRandom(seed))
		}
	})
}

func TestWanderingTraderSpawnerDoesNotWaitForGeneration(t *testing.T) {
	t.Parallel()
	w, g := gatedWorld(t, 2, 6000)
	mustNotWait(t, w, g, "the wandering trader spawner", func(tx *world.Tx) {
		for seed := range int64(300) {
			entity.WanderingTraderSpawnTick(tx, entity.SpawnerRandom(seed))
		}
	})
}

func TestVillageSiegeDoesNotWaitForGeneration(t *testing.T) {
	t.Parallel()
	// The zombies spawn 32 blocks from the player, at the edge of the
	// generated area.
	w, g := gatedWorld(t, 2, 18001)
	mustNotWait(t, w, g, "the village siege", func(tx *world.Tx) {
		for i := range 6 {
			head := cube.Pos{-10 + 4*i, 64, -20}
			tx.SetBlock(head.Side(cube.FaceSouth), block.Bed{Facing: cube.North}, nil)
			tx.SetBlock(head, block.Bed{Facing: cube.North, Head: true}, nil)
			tx.POIs().Claim(head, uuid.New())
		}
		for seed := range int64(100) {
			entity.VillageSiegeTick(tx, entity.SpawnerRandom(seed))
		}
	})
}
