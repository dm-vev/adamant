package chunk_test

import (
	"math/rand/v2"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// TestBulkReadsMatchSingleReads fills chunks with blocks, liquids and source
// states at random and checks Native and HighestBlocks return what Block,
// StateID and HighestBlock return block by block.
func TestBulkReadsMatchSingleReads(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	reg := world.DefaultBlockRegistry
	var rids []uint32
	for _, name := range []string{"minecraft:stone", "minecraft:dirt", "minecraft:water", "minecraft:oak_log"} {
		rid, ok := reg.StateToRuntimeID(name, nil)
		if !ok {
			rid, ok = reg.StateToRuntimeID(name, map[string]any{"pillar_axis": "y"})
		}
		if !ok {
			t.Fatalf("%s runtime ID missing", name)
		}
		rids = append(rids, rid)
	}
	var ids []uint32
	for _, axis := range []string{"x", "z"} {
		id, err := gamedata.BlockStateID("minecraft:oak_log", gamedata.BlockPropertiesWithDefaults("minecraft:oak_log", map[string]any{"axis": axis}))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	r := rand.New(rand.NewPCG(7, 8))
	for layout := range 20 {
		c := chunk.New(reg, cube.Range{-64, 319})
		for range 50 + layout*200 {
			x, y, z := uint8(r.IntN(16)), int16(r.IntN(384)-64), uint8(r.IntN(16))
			if layout%3 == 0 {
				// Keep the blocks low, leaving the sub chunks above empty.
				y = int16(r.IntN(40) - 64)
			}
			c.SetBlock(x, y, z, 0, rids[r.IntN(len(rids))])
			switch r.IntN(4) {
			case 0:
				c.SetBlock(x, y, z, 1, rids[2])
			case 1:
				c.SetJavaStateID(x, y, z, ids[r.IntN(len(ids))])
			case 2:
				c.SetBlock(x, y, z, 0, reg.AirRuntimeID())
			}
		}
		var heights [256]int16
		c.HighestBlocks(&heights)
		for x := range uint8(16) {
			for z := range uint8(16) {
				if got, want := heights[int(x)<<4|int(z)], c.HighestBlock(x, z); got != want {
					t.Fatalf("layout %d: highest block of %v %v %d, want %d", layout, x, z, got, want)
				}
				for y := int16(-64); y < 320; y++ {
					b, liquid, id := c.Native(x, y, z)
					if b != c.Block(x, y, z, 0) || liquid != c.Block(x, y, z, 1) || id != c.StateID(x, y, z) {
						t.Fatalf("layout %d: %v %v %v read as %v %v %v, want %v %v %v", layout, x, y, z, b, liquid, id,
							c.Block(x, y, z, 0), c.Block(x, y, z, 1), c.StateID(x, y, z))
					}
				}
			}
		}
	}
}
