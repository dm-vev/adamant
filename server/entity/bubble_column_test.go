package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestBubbleColumnAppliesThroughEntityInsiderPath(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	pos := cube.Pos{0, 64, 0}

	w.Do(func(tx *world.Tx) {
		tx.SetBlock(pos, block.BubbleColumn{}, nil)
		tx.SetLiquid(pos, block.Water{Depth: 8, Still: true})
		tx.SetBlock(pos.Side(cube.FaceUp), block.BubbleColumn{}, nil)
		tx.SetLiquid(pos.Side(cube.FaceUp), block.Water{Depth: 8, Still: true})
		handle := NewItem(world.EntitySpawnOpts{
			Position: mgl64.Vec3{0.5, 64.1, 0.5},
		}, item.NewStack(item.Stick{}, 1))
		e := tx.AddEntity(handle).(*Ent)

		checkEntityInsiders(tx, e)
		if got := e.Velocity()[1]; got != 0.06 {
			t.Fatalf("vertical velocity after bubble contact = %v, want 0.06", got)
		}
	})
}
