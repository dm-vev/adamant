package overworld

import (
	"sync"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// Chunk contents must not depend on worker scheduling. Compare runs using the
// same registry, not pinned hashes of runtime IDs that change with the palette.
// This checks determinism, not equivalence to any Java or Bedrock release.
func TestGenerationOrderDoesNotChangeChunkContents(t *testing.T) {
	positions := []world.ChunkPos{{-1, -1}, {-1, 0}, {0, -1}, {0, 0}}
	r := world.Overworld.Range()
	generate := func(g *Overworld, pos world.ChunkPos) *chunk.Chunk {
		c := chunk.New(world.DefaultBlockRegistry, r)
		g.GenerateChunk(pos, c)
		return c
	}
	baseline := make([]*chunk.Chunk, len(positions))
	g := NewOverworld(0)
	for i, pos := range positions {
		baseline[i] = generate(g, pos)
	}
	for _, parallel := range []bool{false, true} {
		name := "reverse"
		if parallel {
			name = "concurrent"
		}
		t.Run(name, func(t *testing.T) {
			g := NewOverworld(0)
			got := make([]*chunk.Chunk, len(positions))
			var wg sync.WaitGroup
			for i := len(positions) - 1; i >= 0; i-- {
				if parallel {
					wg.Go(func() { got[i] = generate(g, positions[i]) })
				} else {
					got[i] = generate(g, positions[i])
				}
			}
			wg.Wait()
			for i, pos := range positions {
				for y := int16(r.Min()); y <= int16(r.Max()); y++ {
					for x := range uint8(16) {
						for z := range uint8(16) {
							for layer := range uint8(2) {
								if got[i].Block(x, y, z, layer) != baseline[i].Block(x, y, z, layer) {
									t.Fatalf("chunk %v block (%d,%d,%d), layer %d depends on generation order", pos, x, y, z, layer)
								}
							}
							if got[i].Biome(x, y, z) != baseline[i].Biome(x, y, z) {
								t.Fatalf("chunk %v biome (%d,%d,%d) depends on generation order", pos, x, y, z)
							}
						}
					}
				}
			}
		})
	}
}
