package api

import (
	"reflect"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/overworld"
)

// WorldGenRandom provides deterministic per-chunk random numbers inside
// WorldGenHooks.OnDecorate without exposing internal generator packages.
type WorldGenRandom interface {
	NextInt(bound int32) int32
	NextInt32() int32
	NextLong() int64
	NextFloat() float32
	NextDouble() float64
	NextBoolean() bool
	NextGaussian() float64
	Consume(count int)
}

// WorldGenHooks customizes Overworld generation stages cleanly from external
// code. Non-nil hooks must be safe for concurrent invocation across generation
// workers and deterministic for a given world seed and chunk position.
type WorldGenHooks struct {
	// OnBiome overrides the Bedrock biome ID at world coordinates (x, y, z).
	OnBiome func(x, y, z int, defaultBiome uint8) uint8
	// OnBiomes runs once per generated chunk after base biomes are populated.
	OnBiomes func(pos world.ChunkPos, c *chunk.Chunk)
	// OnTerrain runs after base terrain, surface, and carving complete, before
	// features and structures decorate c.
	OnTerrain func(pos world.ChunkPos, c *chunk.Chunk)
	// OnDecorate runs after structures and features finish decorating c with a
	// deterministic per-chunk random stream.
	OnDecorate func(pos world.ChunkPos, c *chunk.Chunk, rng WorldGenRandom)
}

// AttachOverworldHooks installs hooks on g. Passing zero WorldGenHooks clears
// previously installed hooks and restores vanilla generation fast paths.
func AttachOverworldHooks(g *overworld.Overworld, hooks WorldGenHooks) {
	if g == nil {
		return
	}
	native := overworld.Hooks{
		OnBiome:   hooks.OnBiome,
		OnBiomes:  hooks.OnBiomes,
		OnTerrain: hooks.OnTerrain,
	}
	if hooks.OnDecorate != nil {
		field := reflect.ValueOf(&native).Elem().FieldByName("OnDecorate")
		fn := reflect.MakeFunc(field.Type(), func(args []reflect.Value) []reflect.Value {
			pos := args[0].Interface().(world.ChunkPos)
			c := args[1].Interface().(*chunk.Chunk)
			var rng WorldGenRandom
			if !args[2].IsNil() {
				rng, _ = args[2].Interface().(WorldGenRandom)
			}
			hooks.OnDecorate(pos, c, rng)
			return nil
		})
		field.Set(fn)
	}
	g.SetHooks(native)
}

// ClearOverworldHooks removes any custom generation hooks attached to g.
func ClearOverworldHooks(g *overworld.Overworld) {
	if g == nil {
		return
	}
	g.SetHooks(overworld.Hooks{})
}
