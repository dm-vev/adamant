package world_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func TestLightningSeeksTopmostRodWithin128Blocks(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	<-w.Exec(func(tx *world.Tx) {
		center := cube.Pos{0, 64, 0}
		rod := cube.Pos{40, 80, -30}
		tx.SetBlock(rod, block.LightningRod{Facing: cube.FaceUp}, nil)
		if got, ok := tx.FindLightningRod(center); !ok || got != rod {
			t.Fatalf("rod search found %v, %t; want the exposed rod %v", got, ok, rod)
		}
		tx.SetBlock(rod.Side(cube.FaceUp), block.Stone{}, nil)
		if got, ok := tx.FindLightningRod(center); ok {
			t.Fatalf("a covered rod at %v still attracted lightning", got)
		}
		tx.SetBlock(rod.Side(cube.FaceUp), nil, nil)
		far := cube.Pos{200, 64, 0}
		if got, ok := tx.FindLightningRod(far); ok {
			t.Fatalf("rod %v attracted lightning 160 blocks away", got)
		}
	})
}
