package main

import (
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/biome"
	"github.com/df-mc/dragonfly/server/world/generator"
	"github.com/df-mc/dragonfly/server/world/generator/advanced"
	"github.com/df-mc/dragonfly/server/world/mcdb"
	"github.com/df-mc/goleveldb/leveldb"
	"github.com/go-gl/mathgl/mgl64"
)

// pregen writes a world folder with the chunks around the spawn generated.
func pregen(args []string) error {
	fs := flag.NewFlagSet("pregen", flag.ExitOnError)
	dir := fs.String("world", "", "world folder to create; it must not exist")
	kind := fs.String("kind", "flat", "flat: grass on dirt on bedrock; vanilla: the overworld generator with -seed")
	seed := fs.Int64("seed", 1, "world seed")
	radius := fs.Int("radius", 24, "radius in chunks around the spawn to generate")
	_ = fs.Parse(args)
	if *dir == "" {
		return errors.New("pregen: -world is required")
	}
	if _, err := os.Stat(*dir); err == nil {
		return fmt.Errorf("pregen: %s exists", *dir)
	}
	world.DefaultBlockRegistry.Finalize()
	var gen world.Generator
	switch *kind {
	case "flat":
		gen = generator.NewFlat(biome.Plains{}, []world.Block{block.Grass{}, block.Dirt{}, block.Dirt{}, block.Bedrock{}})
	case "vanilla":
		gen = advanced.NewOverworld(*seed)
	default:
		return fmt.Errorf("pregen: unknown kind %q", *kind)
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	db, err := mcdb.Config{Log: log}.Open(*dir)
	if err != nil {
		return err
	}
	s := db.Settings()
	s.ResolveSeed(*seed)
	s.Lock()
	s.Name = "loadtest-" + *kind
	s.Time, s.TimeCycle = 6000, false
	s.WeatherCycle, s.Raining, s.Thundering, s.RainTime, s.ThunderTime = false, false, false, 0, 0
	s.SpawnMobs, s.SpawnMonsters, s.SpawnWardens = false, false, false
	s.DefaultGameMode = world.GameModeCreative
	s.Difficulty = world.DifficultyPeaceful
	s.Unlock()

	w := world.Config{Provider: db, Generator: gen, Dim: world.Overworld, Log: log, Entities: entity.DefaultRegistry, GeneratorWorkers: 4}.New()
	l := world.NewLoader(*radius, w, world.NopViewer{})
	<-w.Exec(func(tx *world.Tx) { l.Move(tx, mgl64.Vec3{0.5, 0, 0.5}) })
	want := chunksWithin(world.ChunkPos{}, int32(*radius))
	start := time.Now()
	for {
		<-w.Exec(func(tx *world.Tx) { l.Load(tx, 64) })
		loaded := 0
		for _, pos := range want {
			if _, ok := l.Chunk(pos); ok {
				loaded++
			}
		}
		if loaded == len(want) {
			break
		}
		if time.Since(start) > 20*time.Minute {
			return fmt.Errorf("pregen: %d of %d chunks loaded after 20 minutes", loaded, len(want))
		}
		time.Sleep(20 * time.Millisecond)
	}
	var spawn cube.Pos
	<-w.Exec(func(tx *world.Tx) {
		spawn = cube.Pos{0, tx.HighestBlock(0, 0) + 1, 0}
		if *kind == "flat" {
			spawn = gen.DefaultSpawn(world.Overworld)
		}
		s.Lock()
		s.Spawn = spawn
		s.Unlock()
		l.Close(tx)
	})
	if err := w.Close(); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "pregen: %d chunks of a %s world in %v, spawn %v\n", len(want), *kind, time.Since(start).Round(time.Millisecond), spawn)

	// Check that every chunk reached the disk.
	db, err = mcdb.Config{Log: log}.Open(*dir)
	if err != nil {
		return err
	}
	defer db.Close()
	missing := 0
	for _, pos := range want {
		if _, err := db.LoadColumn(pos, world.Overworld); errors.Is(err, leveldb.ErrNotFound) {
			missing++
		} else if err != nil {
			return fmt.Errorf("pregen: read back chunk %v: %w", pos, err)
		}
	}
	if missing != 0 {
		return fmt.Errorf("pregen: %d of %d chunks were not saved", missing, len(want))
	}
	return nil
}

// chunksWithin returns the chunk positions a loader of radius r around
// centre loads: those whose distance, rounded, is at most r.
func chunksWithin(centre world.ChunkPos, r int32) []world.ChunkPos {
	var out []world.ChunkPos
	for x := -r; x <= r; x++ {
		for z := -r; z <= r; z++ {
			if chunkDistance(x, z) <= r {
				out = append(out, world.ChunkPos{centre[0] + x, centre[1] + z})
			}
		}
	}
	return out
}
