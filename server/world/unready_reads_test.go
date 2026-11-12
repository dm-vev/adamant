package world_test

import (
	"sync"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/go-gl/mathgl/mgl64"
)

// originOnlyGenerator generates the chunk at the origin at once and holds
// every other until it is released.
type originOnlyGenerator struct {
	release chan struct{}
	once    sync.Once
}

func (g *originOnlyGenerator) GenerateChunk(pos world.ChunkPos, c *chunk.Chunk, _ world.GenerationContext) world.GenerationResult {
	if pos != (world.ChunkPos{}) {
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

func (*originOnlyGenerator) DefaultSpawn(world.Dimension) cube.Pos { return cube.Pos{} }

func (g *originOnlyGenerator) open() { g.once.Do(func() { close(g.release) }) }

// ownerMustNotWait runs f on the owner of a world with only its origin
// chunk generated, and fails the test if it waits for another.
func ownerMustNotWait(t *testing.T, what string, setup, f func(tx *world.Tx)) {
	t.Helper()
	g := &originOnlyGenerator{release: make(chan struct{})}
	w := world.Config{Entities: entity.DefaultRegistry, Generator: g, GeneratorWorkers: 2, Provider: world.NopProvider{}, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { g.open(); _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		tx.Block(cube.Pos{0, 64, 0})
		setup(tx)
	})
	done := w.Exec(f)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Errorf("%s waited on the world owner for a column the generator did not finish", what)
		g.open()
		<-done
	}
}

func TestRaidWaveSearchDoesNotWaitForGeneration(t *testing.T) {
	ownerMustNotWait(t, "the raid spawn search", func(*world.Tx) {}, func(tx *world.Tx) {
		// A raid ends its cooldown 30 s after its start, spawning its
		// wave 200 blocks out.
		for range 20 {
			tx.RaidSpawnPosition(cube.Pos{8, 64, 8}, 600)
		}
	})
}

func TestLightningDoesNotWaitForTheColumnOfAnEntityNextToIt(t *testing.T) {
	ownerMustNotWait(t, "a lightning strike", func(tx *world.Tx) {
		// A player at the edge of the chunk, whose own column is queued.
		tx.AddEntity((world.EntitySpawnOpts{}).New(player.Type, player.Config{Position: mgl64.Vec3{16.5, 80, 8.5}, GameMode: world.GameModeSurvival}))
	}, func(tx *world.Tx) {
		for range 400 {
			tx.World().StrikeLightning(tx, world.ChunkPos{})
		}
	})
}
