package world

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

type biomeSearchGenerator struct {
	target  cube.Pos
	samples int
}

func (g *biomeSearchGenerator) GenerateChunk(ChunkPos, *chunk.Chunk, GenerationContext) GenerationResult {
	return GenerationResult{}
}
func (*biomeSearchGenerator) DefaultSpawn(Dimension) cube.Pos { return cube.Pos{0, 64, 0} }
func (g *biomeSearchGenerator) JavaBiomeAt(x, y, z int32) string {
	g.samples++
	pos := cube.Pos{int(x), int(y), int(z)}
	if pos == g.target {
		return "minecraft:target"
	}
	return "minecraft:plains"
}

func TestFindNearestBiomeSamplesGeneratorGrid(t *testing.T) {
	generator := &biomeSearchGenerator{target: cube.Pos{32, 64, 0}}
	w := Config{Generator: generator, Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	pos, found, err := w.FindNearestBiome("target", cube.Pos{0, 64, 0})
	if err != nil {
		t.Fatal(err)
	}
	if !found || pos != generator.target {
		t.Fatalf("nearest biome position=(%v,%t), want %v", pos, found, generator.target)
	}
	if generator.samples == 0 {
		t.Fatal("biome search did not sample its source")
	}
}
