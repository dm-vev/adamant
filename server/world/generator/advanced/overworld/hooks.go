package overworld

import (
	"github.com/dm-vev/adamant/server/internal/vanilla"
	"github.com/dm-vev/adamant/server/world"
	"github.com/dm-vev/adamant/server/world/chunk"
)

// Hooks customises Overworld generation stages without modifying core generator
// internals. Non-nil hooks must be safe for concurrent invocation across
// generation workers and deterministic for a given world seed and chunk.
// Leaving a hook nil incurs zero overhead for that stage.
type Hooks struct {
	// OnBiome overrides the Bedrock biome ID chosen at world coordinates (x, y, z)
	// given the generator's defaultBiome ID. Runs before OnBiomes, OnTerrain, and
	// chunk decoration.
	OnBiome func(x, y, z int, defaultBiome uint8) uint8
	// OnBiomes runs once per generated chunk after base biomes (and OnBiome, if
	// set) are populated, before OnTerrain and chunk decoration.
	OnBiomes func(pos world.ChunkPos, c *chunk.Chunk)
	// OnTerrain runs right after base terrain, surface, carving, and biomes are
	// copied into c, before features and structures decorate the chunk.
	OnTerrain func(pos world.ChunkPos, c *chunk.Chunk)
	// OnDecorate runs after all structures and features finish decorating c, with
	// a deterministic per-chunk *vanilla.Random seeded from the world seed and pos.
	OnDecorate func(pos world.ChunkPos, c *chunk.Chunk, rng *vanilla.Random)
}

func (h Hooks) isZero() bool {
	return h.OnBiome == nil && h.OnBiomes == nil && h.OnTerrain == nil && h.OnDecorate == nil
}

// SetHooks installs custom generation stage hooks on g. Passing all-nil Hooks
// clears previously installed hooks and restores zero-overhead vanilla paths.
// SetHooks should be configured before generating chunks; it clears cached
// previews and origin records so subsequent chunks observe the updated hooks.
func (g *Overworld) SetHooks(h Hooks) {
	if h.isZero() {
		g.hooks.Store(nil)
	} else {
		cp := h
		g.hooks.Store(&cp)
	}
	g.ReleaseGenerationCaches()
}

// Hooks returns a copy of the currently configured generation hooks.
func (g *Overworld) Hooks() Hooks {
	if h := g.hooks.Load(); h != nil {
		return *h
	}
	return Hooks{}
}

func applyPreDecorationHooks(pos world.ChunkPos, c *chunk.Chunk, h *Hooks) {
	if h.OnBiome != nil {
		baseX, baseZ := int(pos[0])<<4, int(pos[1])<<4
		r := c.Range()
		minY, maxY := int16(r.Min()), int16(r.Max())
		for x := range uint8(16) {
			wx := baseX + int(x)
			for z := range uint8(16) {
				wz := baseZ + int(z)
				for y := minY; y <= maxY; y++ {
					def := uint8(c.Biome(x, y, z))
					if got := h.OnBiome(wx, int(y), wz, def); got != def {
						c.SetBiome(x, y, z, uint32(got))
					}
				}
			}
		}
	}
	if h.OnBiomes != nil {
		h.OnBiomes(pos, c)
	}
	if h.OnTerrain != nil {
		h.OnTerrain(pos, c)
	}
}

func decorationHookRandom(seed int64, pos world.ChunkPos) vanilla.Random {
	r := vanilla.NewWorldgenRandom(seed)
	decSeed := r.SetDecorationSeed(seed, pos[0]<<4, pos[1]<<4)
	r.SetFeatureSeed(decSeed, 0, 100)
	return r
}
