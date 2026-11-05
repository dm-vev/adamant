package overworld

import (
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// cachedEntries returns how many origin records, terrain previews and noise
// biomes the generator keeps.
func cachedEntries(g *Overworld) (records, previews, biomes int) {
	for _, c := range []*originRecordCache{&g.modernFeatures.origins, &g.modernFeatures.contexts} {
		c.mu.Lock()
		records += len(c.entries)
		c.mu.Unlock()
	}
	for _, cache := range []*previewCache{g.previewCache, g.rawCache} {
		for i := range cache.shards {
			s := &cache.shards[i]
			s.mu.Lock()
			previews += len(s.index)
			s.mu.Unlock()
		}
	}
	for i := range g.chunkBiomes.shards {
		s := &g.chunkBiomes.shards[i]
		s.mu.Lock()
		biomes += len(s.biomes)
		s.mu.Unlock()
	}
	return
}

// TestReleasedCachesGiveTheSameChunks checks that a generator that released
// its caches generates a chunk in the same bytes as it did with them, and that
// the release leaves none of what the chunks around it were generated with.
func TestReleasedCachesGiveTheSameChunks(t *testing.T) {
	t.Parallel()
	r := world.Overworld.Range()
	g := NewOverworld(0)
	generate := func(pos world.ChunkPos) *chunk.Chunk {
		c := chunk.New(world.DefaultBlockRegistry, r)
		g.GenerateChunk(pos, c, world.GenerationContext{})
		return c
	}
	pos := world.ChunkPos{0, 0}
	want := generate(pos)
	records, previews, biomes := cachedEntries(g)
	if records == 0 || previews == 0 || biomes == 0 {
		t.Fatalf("a generated chunk left %d records, %d previews and %d noise biomes cached, want some of each", records, previews, biomes)
	}

	g.ReleaseGenerationCaches()
	if records, previews, biomes = cachedEntries(g); records+previews+biomes != 0 {
		t.Fatalf("%d records, %d previews and %d noise biomes still cached after the release", records, previews, biomes)
	}

	got := generate(pos)
	for y := int16(r.Min()); y <= int16(r.Max()); y++ {
		for x := range uint8(16) {
			for z := range uint8(16) {
				for layer := range uint8(2) {
					if got.Block(x, y, z, layer) != want.Block(x, y, z, layer) {
						t.Fatalf("block (%d,%d,%d) layer %d differs after the caches were released", x, y, z, layer)
					}
				}
				if got.Biome(x, y, z) != want.Biome(x, y, z) {
					t.Fatalf("biome (%d,%d,%d) differs after the caches were released", x, y, z)
				}
			}
		}
	}
}
