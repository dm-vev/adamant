package world

import (
	"math"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/go-gl/mathgl/mgl64"
)

const (
	tickBenchmarkTargetChunks = 1000
	tickBenchmarkLoadTimeout  = 2 * time.Minute
	tickBenchmarkEntityCount  = 20000
	tickBenchmarkScheduledN   = 4096
	tickBenchmarkScheduledN2  = 16384
)

// benchmarkTerrainGenerator builds a simple dense terrain profile that still
// contains random-tickable blocks (grass), making tick benchmarks closer to
// production than an all-air world.
type benchmarkTerrainGenerator struct {
	stoneRID uint32
	grassRID uint32
}

func newBenchmarkTerrainGenerator() benchmarkTerrainGenerator {
	// Run alone, a benchmark reads the default registry before a world
	// finalised it.
	DefaultBlockRegistry.Finalize()
	stoneRID, _ := chunk.StateToRuntimeID("minecraft:stone", nil)
	grassRID, _ := chunk.StateToRuntimeID("minecraft:grass", nil)
	return benchmarkTerrainGenerator{
		stoneRID: stoneRID,
		grassRID: grassRID,
	}
}

func (g benchmarkTerrainGenerator) GenerateChunk(_ ChunkPos, c *chunk.Chunk, _ GenerationContext) GenerationResult {
	const surfaceY = 64

	minY := int(c.Range().Min())
	for x := range uint8(16) {
		for z := range uint8(16) {
			for y := minY; y < surfaceY; y++ {
				c.SetBlock(x, int16(y), z, 0, g.stoneRID)
			}
			c.SetBlock(x, surfaceY, z, 0, g.grassRID)
		}
	}
	return GenerationResult{}
}

func (benchmarkTerrainGenerator) DefaultSpawn(Dimension) cube.Pos {
	return cube.Pos{0, 65, 0}
}

func BenchmarkTickLoaded1000Chunks(b *testing.B) {
	w, _ := setupTickBenchmarkWorld(b, 3)
	tk := ticker{interval: time.Second / 20}

	b.ReportAllocs()
	b.ResetTimer()
	<-w.Exec(func(tx *Tx) {
		for i := 0; i < b.N; i++ {
			tk.tick(tx)
		}
	})
	b.StopTimer()
}

func BenchmarkTickLoaded1000ChunksNoRandom(b *testing.B) {
	w, _ := setupTickBenchmarkWorld(b, -1)
	tk := ticker{interval: time.Second / 20}

	b.ReportAllocs()
	b.ResetTimer()
	<-w.Exec(func(tx *Tx) {
		for i := 0; i < b.N; i++ {
			tk.tick(tx)
		}
	})
	b.StopTimer()
}

func BenchmarkTickEntities20000Active(b *testing.B) {
	w := setupEntityBenchmarkWorld(b, tickBenchmarkEntityCount)
	tk := ticker{interval: time.Second / 20}

	b.ReportAllocs()
	b.ResetTimer()
	<-w.Exec(func(tx *Tx) {
		defer reportCPU(b)()
		for i := 0; i < b.N; i++ {
			tk.tickEntities(tx, int64(i+1))
		}
	})
	b.StopTimer()
}

// BenchmarkTickEntities20000Tickers ticks 20,000 entities that implement
// TickerEntity, 16 in each of 1,250 columns, with a tick doing almost
// nothing: the case where the cost of the entity loop itself shows most.
func BenchmarkTickEntities20000Tickers(b *testing.B) {
	w := setupTickerBenchmarkWorld(b, tickBenchmarkEntityCount, 16)
	tk := ticker{interval: time.Second / 20}

	b.ReportAllocs()
	b.ResetTimer()
	<-w.Exec(func(tx *Tx) {
		defer reportCPU(b)()
		for i := 0; i < b.N; i++ {
			tk.tickEntities(tx, int64(i+1))
		}
	})
	b.StopTimer()
}

// reportCPU starts measuring the CPU time of the process and returns the
// function that reports it as cpu-ns/op, where it is measured.
func reportCPU(b *testing.B) func() {
	start := processCPUTime()
	return func() {
		if used := processCPUTime() - start; used > 0 {
			b.ReportMetric(float64(used.Nanoseconds())/float64(b.N), "cpu-ns/op")
		}
	}
}

func BenchmarkScheduledTickQueue4096(b *testing.B) {
	benchmarkScheduledTickQueueN(b, tickBenchmarkScheduledN)
}

func BenchmarkScheduledTickQueue16384(b *testing.B) {
	benchmarkScheduledTickQueueN(b, tickBenchmarkScheduledN2)
}

func benchmarkScheduledTickQueueN(b *testing.B, scheduledCount int) {
	w := setupScheduledQueueBenchmarkWorld(b)
	positions := benchmarkScheduledPositions(scheduledCount)
	blk := benchmarkScheduledBlock{}
	queue := newScheduledTickQueue(0)
	// The ticks are dispatched to the block in the world, air, which is
	// not the block scheduled: the benchmark measures the queue. Positions
	// repeat beyond 4096: each is scheduled once a tick.
	scheduled, distinct := 0, len(slices.Compact(slices.SortedFunc(slices.Values(positions), compareBlockPos)))

	b.ReportAllocs()
	b.ResetTimer()
	<-w.Exec(func(tx *Tx) {
		defer reportCPU(b)()
		for i := 0; i < b.N; i++ {
			for _, pos := range positions {
				if queue.schedule(w.conf.Blocks, pos, blk, time.Second/20) {
					scheduled++
				}
			}
			queue.tick(tx, int64(i+1))
		}
	})
	b.StopTimer()
	if scheduled != b.N*distinct {
		b.Fatalf("scheduled %d ticks, want %d", scheduled, b.N*distinct)
	}
}

func setupTickBenchmarkWorld(b *testing.B, randomTickSpeed int) (*World, int) {
	b.Helper()

	radius, expectedChunks := radiusForTargetChunks(tickBenchmarkTargetChunks)
	conf := Config{
		Dim:              Overworld,
		Provider:         NopProvider{},
		Generator:        newBenchmarkTerrainGenerator(),
		GeneratorWorkers: runtime.GOMAXPROCS(0),
		RandomTickSpeed:  randomTickSpeed,
	}
	w := conf.New()
	b.Cleanup(func() {
		if err := w.Close(); err != nil {
			b.Fatalf("failed closing benchmark world: %v", err)
		}
	})
	<-w.Exec(func(tx *Tx) {
		// Weather-dependent logic can touch biome data. The synthetic benchmark
		// generator does not populate biome IDs, so disable weather progression
		// to keep long benchmark runs deterministic and stable.
		w.set.Lock()
		w.set.WeatherCycle = false
		w.set.Raining = false
		w.set.Thundering = false
		w.set.Unlock()
	})

	loader := NewLoader(radius, w, NopViewer{})
	<-w.Exec(func(tx *Tx) {
		loader.Move(tx, mgl64.Vec3{})
	})

	deadline := time.Now().Add(tickBenchmarkLoadTimeout)
	for {
		<-w.Exec(func(tx *Tx) {
			loader.Load(tx, 4096)
		})

		loader.mu.RLock()
		queueLen := len(loader.loadQueue)
		loaded := len(loader.loaded)
		loader.mu.RUnlock()

		if queueLen == 0 {
			if loaded < expectedChunks {
				b.Fatalf("loaded fewer chunks than expected: got=%d expected_at_least=%d", loaded, expectedChunks)
			}
			b.Logf("tick benchmark world ready: radius=%d chunks=%d random_tick_speed=%d", radius, loaded, randomTickSpeed)
			return w, loaded
		}
		if time.Now().After(deadline) {
			b.Fatalf("timed out loading benchmark chunks: radius=%d loaded=%d queue=%d", radius, loaded, queueLen)
		}
		time.Sleep(time.Millisecond)
	}
}

func setupEntityBenchmarkWorld(b *testing.B, entityCount int) *World {
	b.Helper()

	conf := Config{
		Dim:              Overworld,
		Provider:         NopProvider{},
		Generator:        NopGenerator{},
		GeneratorWorkers: runtime.GOMAXPROCS(0),
		RandomTickSpeed:  -1,
	}
	w := conf.New()
	b.Cleanup(func() {
		if err := w.Close(); err != nil {
			b.Fatalf("failed closing entity benchmark world: %v", err)
		}
	})
	<-w.Exec(func(tx *Tx) {
		w.set.Lock()
		w.set.WeatherCycle = false
		w.set.Raining = false
		w.set.Thundering = false
		w.set.Unlock()
	})

	loader := NewLoader(2, w, NopViewer{})
	<-w.Exec(func(tx *Tx) {
		loader.Move(tx, mgl64.Vec3{})
	})
	deadline := time.Now().Add(tickBenchmarkLoadTimeout)
	for {
		<-w.Exec(func(tx *Tx) {
			loader.Load(tx, 1024)
		})
		loader.mu.RLock()
		queueLen := len(loader.loadQueue)
		loader.mu.RUnlock()
		if queueLen == 0 {
			break
		}
		if time.Now().After(deadline) {
			b.Fatalf("timed out loading entity benchmark chunks: queue=%d", queueLen)
		}
		time.Sleep(time.Millisecond)
	}

	etype := benchmarkEntityType{}
	ecfg := benchmarkEntityConfig{}
	<-w.Exec(func(tx *Tx) {
		for i := 0; i < entityCount; i++ {
			x := float64(i&15) + 0.5
			z := float64((i>>4)&15) + 0.5
			h := EntitySpawnOpts{Position: mgl64.Vec3{x, 64, z}}.New(etype, ecfg)
			tx.AddEntity(h)
		}
	})
	b.Logf("entity benchmark world ready: active_entities=%d", entityCount)
	return w
}

// setupTickerBenchmarkWorld returns a synchronous world, whose columns
// all tick their entities, with entityCount ticker entities, perColumn in
// each column of a square of columns.
func setupTickerBenchmarkWorld(b *testing.B, entityCount, perColumn int) *World {
	b.Helper()
	w := Config{
		Dim:             Overworld,
		Provider:        NopProvider{},
		Generator:       NopGenerator{},
		RandomTickSpeed: -1,
		Synchronous:     true,
	}.New()
	b.Cleanup(func() {
		if err := w.Close(); err != nil {
			b.Fatalf("failed closing ticker benchmark world: %v", err)
		}
	})
	columns := (entityCount + perColumn - 1) / perColumn
	side := int(math.Ceil(math.Sqrt(float64(columns))))
	<-w.Exec(func(tx *Tx) {
		for c := range columns {
			tx.chunk(ChunkPos{int32(c % side), int32(c / side)})
		}
		for i := range entityCount {
			c, j := i/perColumn, i%perColumn
			x := float64((c%side)*16+(j%4)*4) + 0.5
			z := float64((c/side)*16+(j/4%4)*4) + 0.5
			tx.AddEntity(EntitySpawnOpts{Position: mgl64.Vec3{x, 64, z}}.New(benchmarkTickerType{}, benchmarkEntityConfig{}))
		}
	})
	b.Logf("ticker benchmark world ready: entities=%d columns=%d", entityCount, columns)
	return w
}

// benchmarkScheduledRegistry is a registry with the block the benchmarks of
// the queue of scheduled ticks schedule.
var benchmarkScheduledRegistry = sync.OnceValue(func() BlockRegistry {
	registry := NewBlockRegistry()
	registry.RegisterBlockState(BlockState{Name: "benchmark:scheduled", Properties: map[string]any{}})
	registry.RegisterBlock(benchmarkScheduledBlock{})
	return registry
})

func setupScheduledQueueBenchmarkWorld(b *testing.B) *World {
	b.Helper()

	conf := Config{
		Dim:              Overworld,
		Provider:         NopProvider{},
		Generator:        NopGenerator{},
		GeneratorWorkers: runtime.GOMAXPROCS(0),
		RandomTickSpeed:  -1,
		Blocks:           benchmarkScheduledRegistry(),
	}
	w := conf.New()
	b.Cleanup(func() {
		if err := w.Close(); err != nil {
			b.Fatalf("failed closing scheduled queue benchmark world: %v", err)
		}
	})
	<-w.Exec(func(tx *Tx) {
		w.set.Lock()
		w.set.WeatherCycle = false
		w.set.Raining = false
		w.set.Thundering = false
		w.set.Unlock()
	})

	loader := NewLoader(1, w, NopViewer{})
	<-w.Exec(func(tx *Tx) {
		loader.Move(tx, mgl64.Vec3{})
	})
	deadline := time.Now().Add(tickBenchmarkLoadTimeout)
	for {
		<-w.Exec(func(tx *Tx) {
			loader.Load(tx, 1024)
		})
		loader.mu.RLock()
		queueLen := len(loader.loadQueue)
		loader.mu.RUnlock()
		if queueLen == 0 {
			break
		}
		if time.Now().After(deadline) {
			b.Fatalf("timed out loading scheduled queue benchmark chunks: queue=%d", queueLen)
		}
		time.Sleep(time.Millisecond)
	}

	return w
}

type benchmarkEntityType struct{}

func (benchmarkEntityType) Open(_ *Tx, handle *EntityHandle, data *EntityData) Entity {
	return benchmarkEntity{handle: handle, data: data}
}

func (benchmarkEntityType) EncodeEntity() string                  { return "benchmark:entity" }
func (benchmarkEntityType) BBox(Entity) cube.BBox                 { return cube.Box(-0.3, 0, -0.3, 0.3, 1.8, 0.3) }
func (benchmarkEntityType) DecodeNBT(map[string]any, *EntityData) {}
func (benchmarkEntityType) EncodeNBT(*EntityData) map[string]any  { return nil }

type benchmarkEntity struct {
	handle *EntityHandle
	data   *EntityData
}

func (e benchmarkEntity) Close() error            { return nil }
func (e benchmarkEntity) H() *EntityHandle        { return e.handle }
func (e benchmarkEntity) Position() mgl64.Vec3    { return e.data.Pos }
func (e benchmarkEntity) Rotation() cube.Rotation { return e.data.Rot }

// benchmarkTickerType is an entity type whose entities tick, doing almost
// nothing.
type benchmarkTickerType struct{ benchmarkEntityType }

func (benchmarkTickerType) Open(_ *Tx, handle *EntityHandle, data *EntityData) Entity {
	return benchmarkTicker{benchmarkEntity{handle: handle, data: data}}
}

func (benchmarkTickerType) EncodeEntity() string { return "benchmark:ticker" }

type benchmarkTicker struct{ benchmarkEntity }

func (e benchmarkTicker) Tick(_ *Tx, tick int64) { e.data.Age += time.Duration(tick & 1) }

type benchmarkEntityConfig struct{}

func (benchmarkEntityConfig) Apply(*EntityData) {}

type benchmarkScheduledBlock struct{}

func (benchmarkScheduledBlock) EncodeBlock() (string, map[string]any) {
	return "benchmark:scheduled", nil
}
func (benchmarkScheduledBlock) Hash() (uint64, uint64) { return 0xBADC0DE, 1 }
func (benchmarkScheduledBlock) Model() BlockModel      { return unknownModel{} }

func benchmarkScheduledPositions(count int) []cube.Pos {
	positions := make([]cube.Pos, count)
	for i := 0; i < count; i++ {
		x := i & 15
		z := (i >> 4) & 15
		y := (i >> 8) & 15
		positions[i] = cube.Pos{x, y, z}
	}
	return positions
}

func radiusForTargetChunks(target int) (radius int, chunkCount int) {
	bestDiff := math.MaxInt
	for r := 1; r <= 64; r++ {
		count := chunksInRoundedRadius(r)
		diff := count - target
		if diff < 0 {
			diff = -diff
		}
		if diff < bestDiff {
			bestDiff = diff
			radius = r
			chunkCount = count
		}
	}
	return radius, chunkCount
}

func chunksInRoundedRadius(r int) int {
	var count int
	for x := -r; x <= r; x++ {
		for z := -r; z <= r; z++ {
			if int(math.Round(math.Sqrt(float64(x*x+z*z)))) <= r {
				count++
			}
		}
	}
	return count
}
