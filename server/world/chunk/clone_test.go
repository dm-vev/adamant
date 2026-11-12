package chunk_test

import (
	"math/rand/v2"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// TestCloneBlocksCopiesBlocksWithoutLight checks that CloneBlocks copies the
// blocks and biomes of a lit chunk, keeps its blocks when the chunk changes
// after, and holds no light.
func TestCloneBlocksCopiesBlocksWithoutLight(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	r := world.Overworld.Range()
	c := randomTerrain(rand.New(rand.NewPCG(3, 4)), r)
	chunk.LightArea([]*chunk.Chunk{c}, 0, 0).Fill()
	clone := c.CloneBlocks()

	for x := range uint8(16) {
		for z := range uint8(16) {
			for y := int16(r.Min()); y <= int16(r.Max()); y++ {
				for layer := range uint8(2) {
					if got, want := clone.Block(x, y, z, layer), c.Block(x, y, z, layer); got != want {
						t.Fatalf("block %v %v %v (layer %v) of the copy = %v, want %v", x, y, z, layer, got, want)
					}
				}
				if got, want := clone.Biome(x, y, z), c.Biome(x, y, z); got != want {
					t.Fatalf("biome at %v %v %v of the copy = %v, want %v", x, y, z, got, want)
				}
			}
		}
	}
	before := clone.Block(1, 0, 1, 0)
	c.SetBlock(1, 0, 1, 0, world.BlockRuntimeID(block.Dirt{}))
	if clone.Block(1, 0, 1, 0) != before {
		t.Error("the copy changed with the chunk")
	}
	for i := range clone.Sub() {
		if sky, light := chunk.SharesLight(clone, i); !sky || !light {
			t.Fatalf("sub-chunk %d of the copy holds light of its own", i)
		}
	}
	if clone.SkyLight(1, int16(r.Max()), 1) != 0 {
		t.Error("the copy holds skylight")
	}
}
