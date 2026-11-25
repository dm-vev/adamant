package worldgen_test

import (
	"image/color"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"

	"github.com/df-mc/dragonfly/sdk/worldgen"
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/end"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/nether"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/overworld"
)

func TestUnifiedHooksBiomesAndStructures(t *testing.T) {
	if err := worldgen.Attach(nil, worldgen.Hooks{}); err == nil {
		t.Fatal("expected error for nil generator")
	}
	if err := worldgen.Attach("not-a-gen", worldgen.Hooks{}); err == nil {
		t.Fatal("expected error for unsupported generator type")
	}

	world.DefaultBlockRegistry.Finalize()

	// Verify unified Attach / Clear on Overworld, Nether, and End.
	var owTerrain, netherDeco, endDeco atomic.Int32
	ow := overworld.NewOverworld(12345)
	if err := worldgen.Attach(ow, worldgen.Hooks{
		OnTerrain: func(world.ChunkPos, *chunk.Chunk) { owTerrain.Add(1) },
	}); err != nil {
		t.Fatalf("Attach(overworld): %v", err)
	}
	cOW := chunk.New(world.DefaultBlockRegistry, world.Overworld.Range())
	ow.GenerateChunk(world.ChunkPos{0, 0}, cOW, world.GenerationContext{})
	if owTerrain.Load() == 0 {
		t.Fatal("overworld OnTerrain hook did not run")
	}
	if err := worldgen.Clear(ow); err != nil {
		t.Fatalf("Clear(overworld): %v", err)
	}

	ne := nether.NewNether(12345)
	if err := worldgen.Attach(ne, worldgen.Hooks{
		OnDecorate: func(_ world.ChunkPos, _ *chunk.Chunk, rng worldgen.Random) {
			if rng != nil {
				_ = rng.NextInt(10)
			}
			netherDeco.Add(1)
		},
	}); err != nil {
		t.Fatalf("Attach(nether): %v", err)
	}
	cNe := chunk.New(world.DefaultBlockRegistry, world.Nether.Range())
	ne.GenerateChunk(world.ChunkPos{0, 0}, cNe, world.GenerationContext{})
	if netherDeco.Load() == 0 {
		t.Fatal("nether OnDecorate hook did not run")
	}
	if err := worldgen.Clear(ne); err != nil {
		t.Fatalf("Clear(nether): %v", err)
	}

	en := end.NewEnd(12345)
	if err := worldgen.Attach(en, worldgen.Hooks{
		OnDecorate: func(_ world.ChunkPos, _ *chunk.Chunk, rng worldgen.Random) {
			if rng != nil {
				_ = rng.NextInt(10)
			}
			endDeco.Add(1)
		},
	}); err != nil {
		t.Fatalf("Attach(end): %v", err)
	}
	cEn := chunk.New(world.DefaultBlockRegistry, world.End.Range())
	en.GenerateChunk(world.ChunkPos{0, 0}, cEn, world.GenerationContext{})
	if endDeco.Load() == 0 {
		t.Fatal("end OnDecorate hook did not run")
	}
	if err := worldgen.Clear(en); err != nil {
		t.Fatalf("Clear(end): %v", err)
	}

	// Custom biome registration & lookup.
	custom := worldgen.NewBiome(249, "sdk_test_crystal_grove",
		worldgen.Temperature(0.6),
		worldgen.Rainfall(0.4),
		worldgen.Precipitation(true),
		worldgen.WaterColour(color.RGBA{R: 10, G: 20, B: 255, A: 255}),
		worldgen.Tags("overworld", "crystal"),
	)
	worldgen.RegisterBiome(custom)
	if got, ok := worldgen.BiomeByID(249); !ok || got.String() != "sdk_test_crystal_grove" {
		t.Fatalf("BiomeByID(249) = (%v, %v)", got, ok)
	}
	if got, ok := worldgen.BiomeByName("sdk_test_crystal_grove"); !ok || got.EncodeBiome() != 249 {
		t.Fatalf("BiomeByName = (%v, %v)", got, ok)
	}

	// Structure placement in world and in chunk with rotation + IgnoreAir.
	tmpl := worldgen.NewStructure("pillar",
		worldgen.At(cube.Pos{0, 0, 0}, block.Stone{}),
		worldgen.At(cube.Pos{1, 0, 0}, block.Air{}),
		worldgen.At(cube.Pos{2, 0, 0}, block.Gold{}),
	)
	if tmpl.Name() != "pillar" || tmpl.Size() != (cube.Pos{3, 1, 1}) {
		t.Fatalf("Structure size=%v name=%q", tmpl.Size(), tmpl.Name())
	}

	c := chunk.New(world.DefaultBlockRegistry, world.Overworld.Range())
	nChunk := tmpl.PlaceInChunk(world.ChunkPos{0, 0}, c, cube.Pos{2, 64, 2}, worldgen.PlaceOpts{
		Rotation:  worldgen.Rotate90,
		IgnoreAir: true,
	})
	if nChunk != 2 {
		t.Fatalf("PlaceInChunk placed %d blocks, want 2", nChunk)
	}

	w := world.Config{
		Log:             slog.New(slog.NewTextHandler(io.Discard, nil)),
		Synchronous:     true,
		RandomTickSpeed: -1,
		Generator:       world.NopGenerator{},
		Provider:        world.NopProvider{},
		Blocks:          world.DefaultBlockRegistry,
		Entities:        entity.DefaultRegistry,
	}.New()
	t.Cleanup(func() { _ = w.Close() })

	<-w.Exec(func(tx *world.Tx) {
		n := tmpl.Place(tx, cube.Pos{4, 64, 4}, worldgen.PlaceOpts{IgnoreAir: true})
		if n != 2 {
			t.Fatalf("Place placed %d blocks, want 2", n)
		}
		if _, ok := tx.Block(cube.Pos{6, 64, 4}).(block.Gold); !ok {
			t.Fatalf("expected Gold at (6,64,4), got %T", tx.Block(cube.Pos{6, 64, 4}))
		}
	})
}
