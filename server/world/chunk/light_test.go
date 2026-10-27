package chunk_test

import (
	"math/rand/v2"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// TestSpreadMatchesReference spreads the light of random 3x3 areas of
// terrain with caves, emitters, liquids and blocks that filter light, and
// checks every light level and the chunks reported changed against
// spreading block by block along every edge.
func TestSpreadMatchesReference(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	r := world.Overworld.Range()
	for seed := uint64(1); seed <= 6; seed++ {
		rng := rand.New(rand.NewPCG(seed, seed*7))
		var area, reference []*chunk.Chunk
		for range 9 {
			c := randomTerrain(rng, r)
			chunk.LightArea([]*chunk.Chunk{c}, 0, 0).Fill()
			area, reference = append(area, c), append(reference, c.Clone())
		}
		got := chunk.LightArea(area, -1, -1).Spread()
		want := chunk.LightArea(reference, -1, -1).SpreadReference()
		if got != want {
			t.Errorf("seed %d: changed chunks %09b, want %09b", seed, got, want)
		}
		for i := range area {
			for y := r.Min(); y <= r.Max(); y++ {
				for x := uint8(0); x < 16; x++ {
					for z := uint8(0); z < 16; z++ {
						if gs, ws := area[i].SkyLight(x, int16(y), z), reference[i].SkyLight(x, int16(y), z); gs != ws {
							t.Fatalf("seed %d chunk %d at %v %v %v: skylight %v, want %v", seed, i, x, y, z, gs, ws)
						}
						if gb, wb := area[i].Light(x, int16(y), z), reference[i].Light(x, int16(y), z); gb != wb {
							t.Fatalf("seed %d chunk %d at %v %v %v: light %v, want %v", seed, i, x, y, z, gb, wb)
						}
					}
				}
			}
		}
	}
}

// TestFillSharesUniformLight checks the sub chunks of filled terrain that
// are all stone below its surface or all air above it share their light, so
// that spreading skips the edges between them.
func TestFillSharesUniformLight(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	r := world.Overworld.Range()
	c := chunk.New(world.DefaultBlockRegistry, r)
	stone := world.BlockRuntimeID(block.Stone{})
	for x := range uint8(16) {
		for z := range uint8(16) {
			for y := int16(r.Min()); y < 64; y++ {
				c.SetBlock(x, y, z, 0, stone)
			}
		}
	}
	chunk.LightArea([]*chunk.Chunk{c}, 0, 0).Fill()
	if sky, light := chunk.SharesLight(c, 1); !sky || !light {
		t.Errorf("sub chunk of stone shares skylight %v and block light %v, want both", sky, light)
	}
	if sky, light := chunk.SharesLight(c, len(c.Sub())-2); !sky || !light {
		t.Errorf("sub chunk of air shares skylight %v and block light %v, want both", sky, light)
	}
}

// randomTerrain returns a chunk of stone up to a random surface with water
// over it here and there, caves, and emitters and blocks filtering light
// scattered through it.
func randomTerrain(rng *rand.Rand, r cube.Range) *chunk.Chunk {
	c := chunk.New(world.DefaultBlockRegistry, r)
	rid := world.BlockRuntimeID
	stone, water, glass := rid(block.Stone{}), rid(block.Water{Depth: 8, Still: true}), rid(block.Glass{})
	special := []uint32{
		rid(block.Glowstone{}), rid(block.Lava{Depth: 8, Still: true}), rid(block.Leaves{Type: block.OakLeaves()}),
		rid(block.Slab{Block: block.Stone{}}), rid(block.Torch{Facing: cube.FaceDown}), glass,
	}
	surface := 40 + rng.IntN(40)
	for x := range uint8(16) {
		for z := range uint8(16) {
			h := surface + rng.IntN(5) - 2
			for y := r.Min(); y < h; y++ {
				switch {
				case y > 10 && y < 30 && rng.IntN(4) == 0:
					// Caves.
				case rng.IntN(60) == 0:
					c.SetBlock(x, int16(y), z, 0, special[rng.IntN(len(special))])
				default:
					c.SetBlock(x, int16(y), z, 0, stone)
				}
			}
			if h < 62 {
				for y := h; y < 62; y++ {
					c.SetBlock(x, int16(y), z, 0, water)
				}
			}
			if rng.IntN(30) == 0 {
				c.SetBlock(x, int16(h+3), z, 0, special[rng.IntN(len(special))])
			}
		}
	}
	return c
}
