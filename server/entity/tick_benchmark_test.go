package entity

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/biome"
	"github.com/df-mc/dragonfly/server/world/generator"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// mobTickBenchGround is the height the mobs of BenchmarkMobTick stand at: the top of the four layers of the flat
// world, which start at the bottom of the Overworld.
var mobTickBenchGround = world.Overworld.Range().Min() + 4

// mobTickBenchPen is the half width of the pen of stone, three blocks high, the mobs of BenchmarkMobTick are kept
// in, so that a long run does not wander out of the loaded chunks.
const mobTickBenchPen = 12

// BenchmarkMobTick measures a tick of one mob in a pen in a flat world at midnight, with the time stopped: one op
// is one call of the mob's Tick, its senses, brain or goals, and movement, without the rest of the world's tick.
// Each mob ticks 100 times before the timer starts, past what it does once it was added.
//
// The random stream and the UUID of each mob are the same in every run, but some choices draw on the random of
// the world, so the allocations of a cow or a zombie vary a little between runs: compare them over several runs,
// or at a few thousand ticks. Timings need a quiet machine.
func BenchmarkMobTick(b *testing.B) {
	world.DefaultBlockRegistry.Finalize()
	w := world.Config{
		Synchronous:     true,
		RandomTickSpeed: -1,
		Entities:        DefaultRegistry,
		Generator:       generator.NewFlat(biome.Plains{}, []world.Block{block.Grass{}, block.Dirt{}, block.Dirt{}, block.Bedrock{}}),
	}.New()
	loader := world.NewLoader(4, w, world.NopViewer{})
	b.Cleanup(func() {
		<-w.Exec(func(tx *world.Tx) { loader.Close(tx) })
		_ = w.Close()
	})
	w.SetTime(18000)
	w.StopTime()
	<-w.Exec(func(tx *world.Tx) {
		loader.Move(tx, mgl64.Vec3{0.5, float64(mobTickBenchGround), 0.5})
		loader.Load(tx, 81)
		for i := -mobTickBenchPen; i <= mobTickBenchPen; i++ {
			for y := mobTickBenchGround; y < mobTickBenchGround+3; y++ {
				for _, pos := range []cube.Pos{{i, y, -mobTickBenchPen}, {i, y, mobTickBenchPen}, {-mobTickBenchPen, y, i}, {mobTickBenchPen, y, i}} {
					tx.SetBlock(pos, block.Stone{}, nil)
				}
			}
		}
	})

	for _, mob := range []struct {
		name string
		new  func(world.EntitySpawnOpts) *world.EntityHandle
	}{
		{"zombie", NewZombie},
		{"villager", NewVillager},
		{"cow", NewCow},
		{"fox", NewFox},
	} {
		b.Run(mob.name, func(b *testing.B) {
			<-w.Exec(func(tx *world.Tx) {
				start := mgl64.Vec3{0.5, float64(mobTickBenchGround), 0.5}
				// The same random stream and UUID in every run, so that the mob makes the same choices.
				random := vanilla.NewXoroshiroRandom(int64(len(mob.name)))
				opts := world.EntitySpawnOpts{Position: start, ID: uuid.NewSHA1(uuid.Nil, []byte(mob.name)), EntityRandom: &random}
				e, ok := tx.AddEntity(mob.new(opts)).(world.TickerEntity)
				if !ok {
					b.Fatalf("a %s does not tick", mob.name)
				}
				defer func() { _ = tx.RemoveEntity(e).Close() }()
				const warm = 100
				for tick := range int64(warm) {
					e.Tick(tx, tick)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := range b.N {
					e.Tick(tx, int64(warm+i))
				}
				b.StopTimer()
				if _, ok := e.H().Entity(tx); !ok {
					b.Fatalf("the %s left the world", mob.name)
				}
				if pos := e.Position(); pos[1] < float64(mobTickBenchGround)-0.01 || max(math.Abs(pos[0]), math.Abs(pos[2])) > mobTickBenchPen {
					b.Fatalf("the %s is out of the pen at %v", mob.name, pos)
				}
			})
		})
	}
}
