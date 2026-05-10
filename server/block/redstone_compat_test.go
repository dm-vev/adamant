package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func TestRedstoneCompatibilityBridge(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	w := world.Config{Generator: world.NopGenerator{}, Provider: world.NopProvider{}}.New()
	defer w.Close()

	<-w.Exec(func(tx *world.Tx) {
		buttonPos := cube.Pos{0, 64, 0}
		tx.SetBlock(buttonPos, Button{Pressed: true}, nil)
		if power := tx.RedstonePower(buttonPos, cube.FaceWest, true); power != 15 {
			t.Errorf("legacy source through conductor API: got power %d, want 15", power)
		}

		sourcePos := cube.Pos{2, 64, 0}
		lampPos := sourcePos.Side(cube.FaceEast)
		tx.SetBlock(sourcePos, RedstoneBlock{}, nil)
		tx.SetBlock(lampPos, RedstoneLamp{}, nil)
		updateRedstoneFrom(lampPos, sourcePos, tx)
		if lamp, ok := tx.Block(lampPos).(RedstoneLamp); !ok || !lamp.Lit {
			t.Errorf("legacy receiver through redstone updater: got %#v, want lit lamp", tx.Block(lampPos))
		}
	})
}
