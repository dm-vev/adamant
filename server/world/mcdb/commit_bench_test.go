package mcdb

import (
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	_ "github.com/dm-vev/adamant/server/block"
	"github.com/dm-vev/adamant/server/world"
	"github.com/dm-vev/adamant/server/world/chunk"
	"github.com/dm-vev/adamant/server/world/generator/advanced"
)

// BenchmarkCheckpointCommit measures the time to store and commit batches of
// realistic generated Overworld columns into a Commit and commit them to disk,
// reporting store-ms, commit-ms, total-ms, and commit-MB.
func BenchmarkCheckpointCommit(b *testing.B) {
	world.DefaultBlockRegistry.Finalize()
	gen := advanced.NewOverworld(1)
	const templates = 8
	var terrain [templates]*chunk.Column
	for i := range templates {
		c := chunk.New(world.DefaultBlockRegistry, world.Overworld.Range())
		gen.GenerateChunk(world.ChunkPos{int32(i * 7), 40}, c, world.GenerationContext{})
		c.Compact()
		terrain[i] = &chunk.Column{
			Chunk: c,
			Entities: []chunk.Entity{
				{
					ID: int64(i*10 + 1),
					Data: map[string]any{
						"identifier": "minecraft:sheep",
						"Pos":        []any{float32(i*16 + 4), float32(70), float32(4)},
					},
				},
			},
		}
	}

	for _, count := range []int{1000, 6000} {
		b.Run(fmt.Sprintf("full_%d", count), func(b *testing.B) {
			var storeDur, commitDur time.Duration
			var commitBytes int64
			for b.Loop() {
				b.StopTimer()
				dir := b.TempDir()
				db, err := Config{Log: slog.New(slog.NewTextHandler(io.Discard, nil))}.Open(dir)
				if err != nil {
					b.Fatal(err)
				}
				b.StartTimer()

				t0 := time.Now()
				c := db.BeginCommit()
				for i := range count {
					pos := world.ChunkPos{int32(i % 100), int32(i / 100)}
					tmpl := terrain[i%templates]
					col := &chunk.Column{
						Chunk: tmpl.Chunk,
						Entities: []chunk.Entity{
							{
								ID: int64(i + 1),
								Data: map[string]any{
									"identifier": "minecraft:sheep",
									"Pos":        []any{float32(pos[0]*16 + 4), float32(70), float32(pos[1]*16 + 4)},
								},
							},
						},
					}
					if err := c.StoreColumn(pos, world.Overworld, col); err != nil {
						b.Fatal(err)
					}
				}
				t1 := time.Now()
				commitBytes = int64(c.(*Commit).Size())
				if err := c.Commit(1); err != nil {
					b.Fatal(err)
				}
				t2 := time.Now()
				storeDur += t1.Sub(t0)
				commitDur += t2.Sub(t1)

				b.StopTimer()
				_ = db.Close()
				b.StartTimer()
			}
			n := float64(b.N)
			b.ReportMetric(float64(storeDur.Milliseconds())/n, "store-ms")
			b.ReportMetric(float64(commitDur.Milliseconds())/n, "commit-ms")
			b.ReportMetric(float64((storeDur+commitDur).Milliseconds())/n, "total-ms")
			b.ReportMetric(float64(commitBytes)/(1<<20), "commit-MB")
		})
	}

	b.Run("update_2000_populated", func(b *testing.B) {
		const count = 2000
		dir := b.TempDir()
		db, err := Config{Log: slog.New(slog.NewTextHandler(io.Discard, nil))}.Open(dir)
		if err != nil {
			b.Fatal(err)
		}
		defer db.Close()

		// Seed DB with 4,000 columns across an initial checkpoint so SSTables exist.
		seed := db.BeginCommit()
		for i := range count * 2 {
			pos := world.ChunkPos{int32(i % 100), int32(i / 100)}
			tmpl := terrain[i%templates]
			col := &chunk.Column{
				Chunk: tmpl.Chunk,
				Entities: []chunk.Entity{
					{
						ID: int64(i + 1),
						Data: map[string]any{
							"identifier": "minecraft:sheep",
							"Pos":        []any{float32(pos[0]*16 + 4), float32(70), float32(pos[1]*16 + 4)},
						},
					},
				},
			}
			if err := seed.StoreColumn(pos, world.Overworld, col); err != nil {
				b.Fatal(err)
			}
		}
		if err := seed.Commit(1); err != nil {
			b.Fatal(err)
		}

		var storeDur, commitDur time.Duration
		var commitBytes int64
		var id uint64 = 2
		for b.Loop() {
			t0 := time.Now()
			c := db.BeginCommit()
			for i := range count {
				pos := world.ChunkPos{int32((i * 2) % 100), int32((i * 2) / 100)}
				tmpl := terrain[i%templates]
				col := &chunk.Column{
					Chunk: tmpl.Chunk,
					Entities: []chunk.Entity{
						{
							ID: int64(i*2 + 1),
							Data: map[string]any{
								"identifier": "minecraft:sheep",
								"Pos":        []any{float32(pos[0]*16 + 5), float32(70), float32(pos[1]*16 + 5)},
							},
						},
					},
				}
				if i%2 == 0 {
					if err := c.StoreColumn(pos, world.Overworld, col); err != nil {
						b.Fatal(err)
					}
				} else {
					if err := c.(world.ActorStorer).StoreColumnActors(pos, world.Overworld, col); err != nil {
						b.Fatal(err)
					}
				}
			}
			t1 := time.Now()
			commitBytes = int64(c.(*Commit).Size())
			if err := c.Commit(id); err != nil {
				b.Fatal(err)
			}
			id++
			t2 := time.Now()
			storeDur += t1.Sub(t0)
			commitDur += t2.Sub(t1)
		}
		n := float64(b.N)
		b.ReportMetric(float64(storeDur.Milliseconds())/n, "store-ms")
		b.ReportMetric(float64(commitDur.Milliseconds())/n, "commit-ms")
		b.ReportMetric(float64((storeDur+commitDur).Milliseconds())/n, "total-ms")
		b.ReportMetric(float64(commitBytes)/(1<<20), "commit-MB")
	})
}
