package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
)

func countDropped[T world.Item](tx *world.Tx) (n int) {
	_, items := deathDrops(tx)
	for _, it := range items {
		if _, ok := it.Item().(T); ok {
			n += it.Count()
		}
	}
	return n
}

func TestScaffoldingBreaksWhenSupportIsLost(t *testing.T) {
	w := newDeathTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 63, 0}, block.Stone{}, nil)
		tx.SetBlock(cube.Pos{0, 64, 0}, block.Scaffolding{}, nil)
		tx.SetBlock(cube.Pos{1, 64, 0}, block.Scaffolding{Distance: 1}, nil)
		tx.SetBlock(cube.Pos{2, 64, 0}, block.Scaffolding{Distance: 2}, nil)
	})
	for range 3 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) { tx.SetBlock(cube.Pos{0, 64, 0}, nil, nil) })
	// The two remaining blocks count each other's distance up a step per tick
	// until neither is supported.
	for range 20 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		for x := 1; x <= 2; x++ {
			if b := tx.Block(cube.Pos{x, 64, 0}); b != (block.Air{}) {
				t.Fatalf("unsupported scaffolding at x=%d left %#v", x, b)
			}
		}
		if n := countDropped[block.Scaffolding](tx); n != 2 {
			t.Fatalf("broken scaffolding dropped %d, want 2", n)
		}
	})
}

func TestUnsupportedScaffoldingFallsOntoGround(t *testing.T) {
	w := newDeathTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 63, 0}, block.Stone{}, nil)
		tx.SetBlock(cube.Pos{0, 70, 0}, block.Scaffolding{Distance: 7}, nil)
	})
	for range 40 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		if b := tx.Block(cube.Pos{0, 70, 0}); b != (block.Air{}) {
			t.Fatalf("unsupported scaffolding stayed: %#v", b)
		}
		if b := tx.Block(cube.Pos{0, 64, 0}); b != (block.Scaffolding{}) {
			t.Fatalf("fallen scaffolding landed as %#v, want supported scaffolding", b)
		}
	})
}

func TestSnifferEggHatchesSnifflet(t *testing.T) {
	w := newDeathTestWorld(t)
	pos := cube.Pos{0, 64, 0}
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(pos.Side(cube.FaceDown), block.MossBlock{}, nil)
		tx.SetBlock(pos, block.SnifferEgg{}, nil)
	})
	// On moss each stage takes a third of 12000 ticks plus up to 300: two
	// cracks, then the hatch.
	var previous world.Block = block.SnifferEgg{}
	for stage := 1; stage <= 3; stage++ {
		ticks := 0
		for changed := false; !changed; {
			w.AdvanceTick()
			ticks++
			<-w.Exec(func(tx *world.Tx) {
				if b := tx.Block(pos); b != previous {
					changed, previous = true, b
				}
			})
			if ticks > 4300 {
				t.Fatalf("stage %d took over 4300 ticks", stage)
			}
		}
		if ticks < 4000 {
			t.Fatalf("stage %d took %d ticks, want at least 4000", stage, ticks)
		}
	}
	<-w.Exec(func(tx *world.Tx) {
		if got := tx.Block(pos); got != (block.Air{}) {
			t.Fatalf("hatched egg left %#v", got)
		}
		var babies int
		for e := range tx.Entities() {
			if s, ok := e.(*entity.Sniffer); ok && s.Baby() {
				babies++
			}
		}
		if babies != 1 {
			t.Fatalf("egg hatched %d baby sniffers, want 1", babies)
		}
	})
}
