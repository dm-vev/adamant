package populate

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/pmgen/rand"
)

type TallGrass struct {
	Amount int
}

var (
	air       = block.Air{}
	grass     = block.Grass{}
	tallGrass = block.ShortGrass{}
)

func (t TallGrass) Populate(w *world.World, pos world.ChunkPos, _ *chunk.Chunk, r *rand.Random) {
	amount := r.Int31n(2) + int32(t.Amount)
	<-w.Exec(func(tx *world.Tx) {
		for i := int32(0); i < amount; i++ {
			x := int(r.Range(pos[0]*16, pos[0]*16+15))
			z := int(r.Range(pos[1]*16, pos[1]*16+15))
			if y, ok := t.highestWorkableBlock(tx, pos, x, z); ok {
				p := cube.Pos{x, y, z}
				if !inChunk(p, pos) {
					continue
				}
				tx.SetBlock(p, tallGrass, nil)
			}
		}
	})
}

func (t TallGrass) highestWorkableBlock(tx *world.Tx, chunkPos world.ChunkPos, x, z int) (int, bool) {
	var next world.Block
	for y := 127; y >= 0; y-- {
		pos := cube.Pos{x, y, z}
		if !inChunk(pos, chunkPos) {
			continue
		}
		b := next
		if b == nil {
			b = tx.Block(pos)
		}
		nextPos := cube.Pos{x, y - 1, z}
		if !inChunk(nextPos, chunkPos) {
			continue
		}
		next = tx.Block(nextPos)
		if b == air && next == grass {
			return y, true
		}
	}
	return 0, false
}
