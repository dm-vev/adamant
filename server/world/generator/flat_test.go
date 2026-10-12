package generator_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/biome"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator"
)

func TestFlatFillsBiomeAcrossDimensionHeight(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	for _, test := range []struct {
		name string
		dim  world.Dimension
	}{{"overworld", world.Overworld}, {"nether", world.Nether}, {"end", world.End}} {
		t.Run(test.name, func(t *testing.T) {
			dim := test.dim
			c := chunk.New(world.DefaultBlockRegistry, dim.Range())
			g := generator.NewFlat(biome.Desert{}, []world.Block{block.Grass{}, block.Stone{}})
			g.GenerateChunk(world.ChunkPos{-2, 3}, c, world.GenerationContext{})
			for x := range uint8(16) {
				for z := range uint8(16) {
					for y := dim.Range().Min(); y <= dim.Range().Max(); y++ {
						if got := c.Biome(x, int16(y), z); got != uint32(biome.Desert{}.EncodeBiome()) {
							t.Fatalf("biome at %d,%d,%d = %d, want desert", x, y, z, got)
						}
					}
					min := int16(dim.Range().Min())
					for offset, b := range []world.Block{block.Stone{}, block.Grass{}, block.Air{}} {
						if got := c.Block(x, min+int16(offset), z, 0); got != world.BlockRuntimeID(b) {
							t.Fatalf("layer %d at %d,%d = %d, want %T", offset, x, z, got, b)
						}
					}
				}
			}
		})
	}
}
