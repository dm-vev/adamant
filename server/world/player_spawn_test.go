package world_test

import (
	"context"
	"errors"
	"math"
	"sync/atomic"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/go-gl/mathgl/mgl64"
)

func TestPlayerSpawnPositionRespectsRadiusAndAvoidsWater(t *testing.T) {
	t.Parallel()
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	<-w.Exec(func(tx *world.Tx) {
		// A stone floor at y=0 whose western half is covered by water.
		for x := -12; x <= 12; x++ {
			for z := -12; z <= 12; z++ {
				tx.SetBlock(cube.Pos{x, 0, z}, block.Stone{}, nil)
				if x < 0 {
					tx.SetBlock(cube.Pos{x, 1, z}, block.Water{Still: true, Depth: 8}, nil)
				}
			}
		}
		spawn := cube.Pos{0, 5, 0}
		for range 200 {
			pos := tx.PlayerSpawnPosition(spawn)
			if pos[1] != 1 {
				t.Fatalf("spawned at y=%v, want standing on the floor at 1", pos[1])
			}
			if math.Abs(pos[0]-0.5) > 10 || math.Abs(pos[2]-0.5) > 10 {
				t.Fatalf("spawn %v outside respawn_radius 10", pos)
			}
			if pos[0] < 0 {
				t.Fatalf("spawned in water at %v", pos)
			}
		}
		if err := tx.World().SetGameRule("respawn_radius", int64(0)); err != nil {
			t.Fatal(err)
		}
		if pos := tx.PlayerSpawnPosition(spawn); pos != (mgl64.Vec3{0.5, 1, 0.5}) {
			t.Fatalf("respawn_radius 0 spawned at %v, want the spawn column", pos)
		}
	})
}

// slowStoneGenerator generates stone up to y 59 and takes delay for each
// chunk.
type slowStoneGenerator struct {
	delay     time.Duration
	generated atomic.Int64
}

func (g *slowStoneGenerator) GenerateChunk(_ world.ChunkPos, c *chunk.Chunk, _ world.GenerationContext) world.GenerationResult {
	time.Sleep(g.delay)
	stone, _ := chunk.StateToRuntimeID("minecraft:stone", nil)
	for x := range uint8(16) {
		for z := range uint8(16) {
			for y := int16(c.Range().Min()); y < 60; y++ {
				c.SetBlock(x, y, z, 0, stone)
			}
		}
	}
	g.generated.Add(1)
	return world.GenerationResult{}
}

func (*slowStoneGenerator) DefaultSpawn(world.Dimension) cube.Pos { return cube.Pos{8, 400, 8} }

// TestFindPlayerSpawnLetsTheWorldTick finds the spawn of a first join in
// chunks that take a while to generate, and checks the spawn found stands on
// the ground within respawn_radius, while the owner went on running
// transactions as the chunks generated.
func TestFindPlayerSpawnLetsTheWorldTick(t *testing.T) {
	// A chunk takes long enough that an owner waiting for one waits far past
	// half of it, while the race detector on a busy machine delays a
	// transaction by up to about 85 ms with no wait at all.
	const delay = 300 * time.Millisecond
	g := &slowStoneGenerator{delay: delay}
	w := world.Config{Generator: g, Provider: world.NopProvider{}, RandomTickSpeed: -1, GeneratorWorkers: 1}.New()
	defer w.Close()
	spawn := cube.Pos{0, 64, 0}

	stop := make(chan struct{})
	waits := make(chan time.Duration, 1)
	go func() {
		var longest time.Duration
		defer func() { waits <- longest }()
		for {
			select {
			case <-stop:
				return
			case <-time.After(5 * time.Millisecond):
			}
			start := time.Now()
			<-w.Exec(func(*world.Tx) {})
			longest = max(longest, time.Since(start))
		}
	}()
	pos, err := w.FindPlayerSpawn(context.Background(), spawn)
	close(stop)
	longest := <-waits
	if err != nil {
		t.Fatalf("FindPlayerSpawn: %v", err)
	}
	if pos[1] != 60 {
		t.Errorf("spawned at y=%v, want standing on the stone at 60", pos[1])
	}
	if math.Abs(pos[0]-0.5) > 10 || math.Abs(pos[2]-0.5) > 10 {
		t.Errorf("spawn %v outside respawn_radius 10", pos)
	}
	if g.generated.Load() == 0 {
		t.Fatal("no chunk generated for the search")
	}
	if longest >= delay/2 {
		t.Errorf("a transaction waited %v for the owner while chunks taking %v to generate were searched", longest, delay)
	}
}

// TestFindPlayerSpawnFailsOnceTheWorldCloses checks that a search of the
// spawn returns once the world closes, or its context ends.
func TestFindPlayerSpawnFailsOnceTheWorldCloses(t *testing.T) {
	g := &slowStoneGenerator{delay: time.Second}
	w := world.Config{Generator: g, Provider: world.NopProvider{}, RandomTickSpeed: -1, GeneratorWorkers: 1}.New()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := w.FindPlayerSpawn(ctx, cube.Pos{0, 64, 0}); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("FindPlayerSpawn with its context ended = %v, want %v", err, context.DeadlineExceeded)
	}
	errs := make(chan error, 1)
	go func() {
		_, err := w.FindPlayerSpawn(context.Background(), cube.Pos{0, 64, 0})
		errs <- err
	}()
	time.Sleep(50 * time.Millisecond)
	_ = w.Close()
	select {
	case err := <-errs:
		if err != nil && !errors.Is(err, world.ErrWorldClosed) {
			t.Errorf("FindPlayerSpawn once the world closed = %v, want %v", err, world.ErrWorldClosed)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("FindPlayerSpawn did not return once the world closed")
	}
}
