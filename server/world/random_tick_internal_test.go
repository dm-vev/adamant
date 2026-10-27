package world

import (
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
)

// cubeStructure is a sub chunk of the block passed.
type cubeStructure struct{ b Block }

func (cubeStructure) Dimensions() [3]int { return [3]int{16, 16, 16} }
func (s cubeStructure) At(int, int, int, func(int, int, int) Block) (Block, Liquid) {
	return s.b, nil
}

// TestRandomTicksFoundInColumnOrder checks the blocks found to tick randomly
// in columns scanned on all cores are those each column finds on its own,
// in the order of the columns, and that every one of them ticks randomly.
func TestRandomTicksFoundInColumnOrder(t *testing.T) {
	w := Config{Synchronous: true, RandomTickSpeed: -1, Provider: NopProvider{}, Generator: NopGenerator{}}.New()
	defer w.Close()
	<-w.Exec(func(tx *Tx) {
		grass, ok := w.conf.Blocks.BlockByName("minecraft:grass_block", nil)
		if !ok {
			t.Fatal("grass is not registered")
		}
		stone, _ := w.conf.Blocks.BlockByName("minecraft:stone", nil)
		var scan []randomTickScan
		for i := range 70 {
			pos := ChunkPos{int32(i%10) - 5, int32(i/10) - 3}
			// Columns hold a sub chunk full of grass, a layer with grass in
			// every third block, or nothing that ticks.
			if i%3 == 0 {
				tx.BuildStructure(cube.Pos{int(pos[0]) << 4, i % 5 * 16, int(pos[1]) << 4}, cubeStructure{b: grass})
			}
			for x := range 16 {
				for z := range 16 {
					p := cube.Pos{int(pos[0])<<4 | x, i%5*16 + 3, int(pos[1])<<4 | z}
					switch {
					case i%3 == 1 && (x+z)%3 == 0:
						tx.SetBlock(p, grass, nil)
						tx.SetBlock(p.Add(cube.Pos{0, 1, 0}), stone, nil)
					case i%3 == 2:
						tx.SetBlock(p, stone, nil)
					}
				}
			}
			c := tx.chunk(pos)
			if indices := c.randomTickSubChunkIndices(w.conf.Blocks); len(indices) != 0 {
				scan = append(scan, randomTickScan{pos: pos, sub: c.Sub(), indices: indices})
			}
		}
		const speed, seed = 3, 0x5eed
		got := w.findRandomTicks(nil, scan, speed, seed)
		var want []cube.Pos
		for _, s := range scan {
			found := w.findRandomTicks(nil, []randomTickScan{s}, speed, seed)
			if i := s.pos[0] + 5 + (s.pos[1]+3)*10; i%3 == 0 && len(found) != speed {
				t.Fatalf("column %v full of grass found %d blocks to tick, want %d", s.pos, len(found), speed)
			}
			want = append(want, found...)
		}
		if !slices.Equal(got, want) {
			t.Fatalf("found %d blocks to tick, the columns on their own %d", len(got), len(want))
		}
		if len(got) == 0 {
			t.Fatal("no blocks found to tick")
		}
		for _, pos := range got {
			if !w.conf.Blocks.RandomTickBlock(w.conf.Blocks.BlockRuntimeID(tx.Block(pos))) {
				t.Fatalf("block found to tick at %v does not tick randomly", pos)
			}
		}
		if again := w.findRandomTicks(nil, scan, speed, seed+1); slices.Equal(again, got) {
			t.Fatal("another seed found the same blocks to tick")
		}
	})
}
