package nether

import (
	"sync/atomic"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

const benchNetherSeed = 12345

func findNetherBiomeChunk(g *Nether, target string) world.ChunkPos {
	for r := int32(0); r < 128; r++ {
		for cx := -r; cx <= r; cx++ {
			for _, cz := range [2]int32{-r, r} {
				if g.biomes.Biome(cx*16+8, cz*16+8) == target {
					return world.ChunkPos{cx, cz}
				}
			}
		}
		for cz := -r + 1; cz < r; cz++ {
			for _, cx := range [2]int32{-r, r} {
				if g.biomes.Biome(cx*16+8, cz*16+8) == target {
					return world.ChunkPos{cx, cz}
				}
			}
		}
	}
	return world.ChunkPos{0, 0}
}

func findNetherStructureChunk(g *Nether, kind string) world.ChunkPos {
	for cx := int32(-64); cx <= 64; cx++ {
		for cz := int32(-64); cz <= 64; cz++ {
			switch kind {
			case "fortress", "bastion":
				start, err := g.complexStart(cx, cz)
				if err != nil || start == nil {
					continue
				}
				if kind == "fortress" && start.name == "minecraft:fortress" {
					return world.ChunkPos{cx, cz}
				}
				if kind == "bastion" && start.name == "minecraft:bastion_remnant" {
					return world.ChunkPos{cx, cz}
				}
			case "portal":
				start, err := g.portalStart(cx, cz)
				if err == nil && start != nil {
					return world.ChunkPos{cx, cz}
				}
			}
		}
	}
	return world.ChunkPos{0, 0}
}

func netherGridPositions(centres [][2]int32, radius int32) []world.ChunkPos {
	side := int(2*radius + 1)
	positions := make([]world.ChunkPos, 0, len(centres)*side*side)
	for _, ctr := range centres {
		for dx := -radius; dx <= radius; dx++ {
			for dz := -radius; dz <= radius; dz++ {
				positions = append(positions, world.ChunkPos{ctr[0] + dx, ctr[1] + dz})
			}
		}
	}
	return positions
}

func BenchmarkGenerateChunk(b *testing.B) {
	world.DefaultBlockRegistry.Finalize()
	r := world.Nether.Range()

	b.Run("DiverseBiomesAndStructures", func(b *testing.B) {
		probe := NewNether(benchNetherSeed)
		targets := []struct {
			name string
			pos  world.ChunkPos
		}{
			{"NetherWastes", findNetherBiomeChunk(probe, "minecraft:nether_wastes")},
			{"SoulSandValley", findNetherBiomeChunk(probe, "minecraft:soul_sand_valley")},
			{"CrimsonForest", findNetherBiomeChunk(probe, "minecraft:crimson_forest")},
			{"WarpedForest", findNetherBiomeChunk(probe, "minecraft:warped_forest")},
			{"BasaltDeltas", findNetherBiomeChunk(probe, "minecraft:basalt_deltas")},
			{"Fortress", findNetherStructureChunk(probe, "fortress")},
			{"Bastion", findNetherStructureChunk(probe, "bastion")},
			{"RuinedPortal", findNetherStructureChunk(probe, "portal")},
		}
		for _, tc := range targets {
			b.Run(tc.name, func(b *testing.B) {
				g := NewNether(benchNetherSeed)
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					c := chunk.New(world.DefaultBlockRegistry, r)
					g.GenerateChunk(tc.pos, c, world.GenerationContext{})
				}
			})
		}
	})

	b.Run("MixedGrid75", func(b *testing.B) {
		positions := netherGridPositions([][2]int32{{0, 0}, {40, -40}, {-200, 120}}, 2)
		g := NewNether(benchNetherSeed)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			pos := positions[i%len(positions)]
			c := chunk.New(world.DefaultBlockRegistry, r)
			g.GenerateChunk(pos, c, world.GenerationContext{})
		}
	})

	b.Run("PrepareTerrain", func(b *testing.B) {
		positions := netherGridPositions([][2]int32{{0, 0}, {40, -40}, {-200, 120}}, 2)
		g := NewNether(benchNetherSeed)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			pos := positions[i%len(positions)]
			c := chunk.New(world.DefaultBlockRegistry, r)
			g.prepareTerrain(pos, c)
		}
	})

	b.Run("FillBiomes", func(b *testing.B) {
		positions := netherGridPositions([][2]int32{{0, 0}, {40, -40}, {-200, 120}}, 2)
		g := NewNether(benchNetherSeed)
		c := chunk.New(world.DefaultBlockRegistry, r)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			pos := positions[i%len(positions)]
			g.fillBiomes(int(pos[0]), int(pos[1]), c)
		}
	})

	b.Run("ParallelGrid", func(b *testing.B) {
		positions := netherGridPositions([][2]int32{{0, 0}, {40, -40}, {-200, 120}}, 2)
		g := NewNether(benchNetherSeed)
		var idx atomic.Uint64
		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				pos := positions[idx.Add(1)%uint64(len(positions))]
				c := chunk.New(world.DefaultBlockRegistry, r)
				g.GenerateChunk(pos, c, world.GenerationContext{})
			}
		})
	})
}
