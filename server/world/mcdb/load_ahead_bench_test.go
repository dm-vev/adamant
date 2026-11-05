package mcdb

import (
	"io"
	"log/slog"
	"math"
	"testing"
	"time"

	_ "github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced"
	"github.com/go-gl/mathgl/mgl64"
)

// blindDB is the database saying it does not load concurrently, so that the
// world reads its columns on its owner, as it does from a provider that does
// not say it does.
type blindDB struct{ *DB }

func (blindDB) LoadsConcurrently() bool { return false }

// BenchmarkLoaderOwnerTime measures the time the owner of a world spends in
// the transactions of a loader that asks for the columns around a player who
// just joined, from a world saved with generated terrain, the loads a tick
// of 50 ms as the game has it. "owner-ms" is the time the owner ran those
// transactions to load all the columns, "wall-ms" the time it took.
func BenchmarkLoaderOwnerTime(b *testing.B) {
	world.DefaultBlockRegistry.Finalize()
	dir := b.TempDir()
	db, err := Open(dir)
	if err != nil {
		b.Fatal(err)
	}
	const radius = 10
	var positions []world.ChunkPos
	for x := -int32(radius); x <= radius; x++ {
		for z := -int32(radius); z <= radius; z++ {
			if int32(math.Round(math.Hypot(float64(x), float64(z)))) <= radius {
				positions = append(positions, world.ChunkPos{x, z})
			}
		}
	}
	// Four columns of generated terrain, stored at every position of the view.
	gen := advanced.NewOverworld(1)
	var terrain [4]*chunk.Column
	for i := range terrain {
		c := chunk.New(world.DefaultBlockRegistry, world.Overworld.Range())
		gen.GenerateChunk(world.ChunkPos{int32(i * 5), 40}, c, world.GenerationContext{})
		terrain[i] = &chunk.Column{Chunk: c}
	}
	for _, pos := range positions {
		if err := db.StoreColumn(pos, world.Overworld, terrain[(pos[0]+pos[1])&3]); err != nil {
			b.Fatal(err)
		}
	}
	_ = db.Close()

	run := func(b *testing.B, hide bool) {
		var owner, wall time.Duration
		for b.Loop() {
			db, err := Open(dir)
			if err != nil {
				b.Fatal(err)
			}
			var p world.Provider = db
			if hide {
				p = blindDB{db}
			}
			w := world.Config{Provider: p, Generator: gen, Entities: entity.DefaultRegistry, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}.New()
			l := world.NewLoader(radius, w, world.NopViewer{})
			start := time.Now()
			for {
				<-w.Exec(func(tx *world.Tx) {
					t0 := time.Now()
					l.Move(tx, mgl64.Vec3{8, 64, 8})
					l.Load(tx, 4)
					owner += time.Since(t0)
				})
				loaded := 0
				for _, pos := range positions {
					if _, ok := l.Chunk(pos); ok {
						loaded++
					}
				}
				if loaded == len(positions) {
					break
				}
				time.Sleep(50 * time.Millisecond)
			}
			wall += time.Since(start)
			_ = w.Close()
			_ = db.Close()
		}
		b.ReportMetric(float64(owner.Milliseconds())/float64(b.N), "owner-ms")
		b.ReportMetric(float64(wall.Milliseconds())/float64(b.N), "wall-ms")
	}
	b.Run("on-owner", func(b *testing.B) { run(b, true) })
	b.Run("read-ahead", func(b *testing.B) { run(b, false) })
}
