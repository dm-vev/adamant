package overworld

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// TestWorldgenHeightsMatchTheScan checks that the WORLDGEN heights kept for
// the generator are those a scan of the base terrain finds, in the target
// chunk and around it, and once the base terrain left the preview cache.
func TestWorldgenHeightsMatchTheScan(t *testing.T) {
	g := NewOverworld(0)
	target := world.ChunkPos{3, -2}
	newRegion := func() *featureRegion {
		c := chunk.New(world.DefaultBlockRegistry, cube.Range{int(modernMinY), int(modernMaxY)})
		return newFeatureRegion(g.modernFeatures, c, target, &world.GenerationResult{})
	}
	scan := func(r *featureRegion, x, z int32, heightmap string) int32 {
		_, mask, _ := heightmapOf(heightmap)
		for y := min(r.MaxY(), r.columnTop(x, z, true)); y >= r.MinY(); y-- {
			if r.f.heightmapFlags(r.c, r.baseState(featurePos{x, y, z}))&mask != 0 {
				return y + 1
			}
		}
		return r.MinY()
	}
	check := func(r *featureRegion) {
		t.Helper()
		for dx := int32(-2); dx <= 2; dx++ {
			for dz := int32(-2); dz <= 2; dz += 2 {
				for i := int32(0); i < 256; i += 7 {
					x, z := (target[0]+dx)<<4+i&15, (target[1]+dz)<<4+i>>4
					for _, heightmap := range []string{"WORLD_SURFACE_WG", "OCEAN_FLOOR_WG"} {
						kind, _, _ := heightmapOf(heightmap)
						if got, want := r.worldgenHeight(x, z, kind), scan(r, x, z, heightmap); got != want {
							t.Fatalf("%s at %d,%d = %d, want %d", heightmap, x, z, got, want)
						}
					}
				}
			}
		}
	}
	check(newRegion())
	g.previewCache.clear()
	check(newRegion())
	if g.worldgenHeights.get(target) == nil {
		t.Fatal("heights of the target chunk were not kept")
	}
	g.ReleaseGenerationCaches()
	if g.worldgenHeights.get(target) != nil {
		t.Fatal("released caches kept the heights")
	}
}

// TestWorldgenHeightCacheForgetsOldest checks that a full shard forgets the
// chunk it was given first.
func TestWorldgenHeightCacheForgetsOldest(t *testing.T) {
	var c worldgenHeightCache
	first := world.ChunkPos{0, 0}
	c.put(first, &worldgenHeights{})
	s := c.shard(first)
	added := 1
	for x := int32(1); added <= worldgenHeightCacheSize/worldgenHeightShards; x++ {
		pos := world.ChunkPos{x, 0}
		if c.shard(pos) != s {
			continue
		}
		c.put(pos, &worldgenHeights{})
		added++
	}
	if c.get(first) != nil {
		t.Fatal("a full shard kept its oldest chunk")
	}
	if n := len(s.entries); n != worldgenHeightCacheSize/worldgenHeightShards {
		t.Fatalf("shard holds %d chunks, want %d", n, worldgenHeightCacheSize/worldgenHeightShards)
	}
}
