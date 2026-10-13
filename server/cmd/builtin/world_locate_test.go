package builtin

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

type locateStructureGenerator struct {
	location   world.StructureLocation
	calls      int
	biomeCalls int
}

func (g *locateStructureGenerator) GenerateChunk(world.ChunkPos, *chunk.Chunk, world.GenerationContext) world.GenerationResult {
	return world.GenerationResult{}
}
func (*locateStructureGenerator) DefaultSpawn(world.Dimension) cube.Pos { return cube.Pos{0, 64, 0} }
func (g *locateStructureGenerator) FindNearestMapFeature(_ string, _ cube.Pos, _ int, _ func(world.StructureLocation) bool) (world.StructureLocation, bool, error) {
	g.calls++
	return g.location, true, nil
}
func (g *locateStructureGenerator) FindStructureAt(_ string, _ cube.Pos) (world.StructureLocation, bool, error) {
	return g.location, true, nil
}
func (g *locateStructureGenerator) JavaBiomeAt(int32, int32, int32) string {
	g.biomeCalls++
	return "minecraft:plains"
}

func TestLocateBiomeCommandUsesGeneratorSource(t *testing.T) {
	generator := &locateStructureGenerator{}
	w := world.Config{Generator: generator, Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	<-w.Exec(func(tx *world.Tx) {
		(locateCommand{Kind: "biome", Target: "plains"}).Run(nil, &cmd.Output{}, tx)
	})
	if generator.biomeCalls == 0 {
		t.Fatal("biome locate did not sample generator source")
	}
}

func TestLocateStructureCommandUsesWorldStructureLocator(t *testing.T) {
	generator := &locateStructureGenerator{location: world.StructureLocation{ID: "minecraft:village_plains", Position: cube.Pos{40, 70, -24}}}
	w := world.Config{Generator: generator, Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	<-w.Exec(func(tx *world.Tx) {
		(locateCommand{Kind: "structure", Target: "minecraft:village_plains"}).Run(nil, &cmd.Output{}, tx)
	})
	if generator.calls != 1 {
		t.Fatalf("structure lookup calls=%d, want 1", generator.calls)
	}
}
