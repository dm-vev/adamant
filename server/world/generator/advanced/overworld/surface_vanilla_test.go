package overworld

import (
	"testing"
	_ "unsafe"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/mc112"
	mcbiome "github.com/df-mc/dragonfly/server/world/generator/advanced/overworld/internal/biome"
)

func init() {
	worldFinaliseBlockRegistry()
}

//go:linkname worldFinaliseBlockRegistry github.com/df-mc/dragonfly/server/world.finaliseBlockRegistry
func worldFinaliseBlockRegistry()

func findChunkWithCenterBiome(g *Overworld, targets map[int]struct{}, radiusChunks int) (chunkX, chunkZ int, ok bool) {
	for x := -radiusChunks; x <= radiusChunks; x++ {
		for z := -radiusChunks; z <= radiusChunks; z++ {
			id := g.biomeProvider.biomes(x*16+8, z*16+8, 1, 1)[0]
			if _, ok := targets[id]; ok {
				return x, z, true
			}
		}
	}
	return 0, 0, false
}

func biomeDefsForChunk(g *Overworld, chunkX, chunkZ int) (gen [10 * 10]*biomeDef, biomes [16 * 16]*biomeDef) {
	genIDs := g.biomeProvider.biomesForGeneration(chunkX*4-2, chunkZ*4-2, 10, 10)
	biomeIDs := g.biomeProvider.biomes(chunkX*16, chunkZ*16, 16, 16)
	for i, id := range genIDs {
		gen[i] = g.biomeDef(id)
	}
	for i, id := range biomeIDs {
		biomes[i] = g.biomeDef(id)
	}
	return gen, biomes
}

func generateBaseAndSurface(g *Overworld, chunkX, chunkZ int) *chunk.Chunk {
	c := chunk.New(g.airRID, cube.Range{0, 255})
	s := &scratch{
		heightMap:  make([]float64, 5*33*5),
		mainNoise:  make([]float64, 5*33*5),
		minNoise:   make([]float64, 5*33*5),
		maxNoise:   make([]float64, 5*33*5),
		depth:      make([]float64, 5*5*1),
		surfaceBuf: make([]float64, 16*16),
	}

	gen, biomes := biomeDefsForChunk(g, chunkX, chunkZ)
	g.setBlocksInChunk(chunkX, chunkZ, c, gen[:], s)
	r := mc112.NewRand(int64(chunkX)*341873128712 + int64(chunkZ)*132897987541)
	g.replaceBiomeBlocks(chunkX, chunkZ, c, biomes[:], r, s)
	return c
}

func TestSurfaceIncludesMesaBands(t *testing.T) {
	g := NewOverworld(1)
	targets := map[int]struct{}{
		int(mcbiome.Badlands):                    {},
		int(mcbiome.BadlandsPlateau):             {},
		int(mcbiome.WoodedBadlandsPlateau):       {},
		int(mcbiome.ErodedBadlands):              {},
		int(mcbiome.ModifiedBadlandsPlateau):     {},
		int(mcbiome.ModifiedWoodedBadlandsPlateau): {},
	}
	// Mesa biomes can be far from the origin depending on the seed and biome generator.
	// Search progressively further to keep this test stable while still validating mesa band generation.
	chunkX, chunkZ, ok := 0, 0, false
	for _, radius := range []int{96, 192, 384, 512} {
		chunkX, chunkZ, ok = findChunkWithCenterBiome(g, targets, radius)
		if ok {
			break
		}
	}
	if !ok {
		t.Fatalf("no mesa biome found within 512 chunks of origin")
	}

	c := generateBaseAndSurface(g, chunkX, chunkZ)
	stained := make(map[uint32]struct{}, len(g.stainedTerracottaRIDs))
	for _, rid := range g.stainedTerracottaRIDs {
		stained[rid] = struct{}{}
	}

	var stainedCount int
	for x := uint8(0); x < 16; x++ {
		for z := uint8(0); z < 16; z++ {
			for y := int16(0); y <= 255; y++ {
				if _, ok := stained[c.Block(x, y, z, 0)]; ok {
					stainedCount++
				}
			}
		}
	}
	if stainedCount == 0 {
		t.Fatalf("expected stained terracotta in mesa chunk, got 0")
	}
}

func TestSurfaceMegaTaigaHasPodzolPatches(t *testing.T) {
	g := NewOverworld(2)
	targets := map[int]struct{}{
		int(mcbiome.GiantTreeTaiga):      {},
		int(mcbiome.GiantTreeTaigaHills): {},
		int(mcbiome.GiantSpruceTaiga):    {},
		int(mcbiome.GiantSpruceTaigaHills): {},
	}
	chunkX, chunkZ, ok := 0, 0, false
	for _, radius := range []int{96, 192, 384, 512} {
		chunkX, chunkZ, ok = findChunkWithCenterBiome(g, targets, radius)
		if ok {
			break
		}
	}
	if !ok {
		t.Fatalf("no mega taiga biome found within 512 chunks of origin")
	}

	c := generateBaseAndSurface(g, chunkX, chunkZ)
	var podzol, grass int
	for x := uint8(0); x < 16; x++ {
		for z := uint8(0); z < 16; z++ {
			y := c.HighestBlock(x, z)
			for y > 0 {
				rid := c.Block(x, y, z, 0)
				if rid != g.airRID && rid != g.waterRID {
					if rid == g.podzolRID {
						podzol++
					}
					if rid == g.grassRID {
						grass++
					}
					break
				}
				y--
			}
		}
	}
	if podzol == 0 {
		t.Fatalf("expected some podzol on mega taiga surface, got 0")
	}
	if grass == 0 {
		t.Fatalf("expected some grass on mega taiga surface, got 0")
	}
}
