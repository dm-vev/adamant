package builtin

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestDataMergeAndGetBlockActor(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	<-w.Exec(func(tx *world.Tx) {
		pos := cube.Pos{0, 64, 0}
		tx.SetBlock(pos, block.NewChest(), nil)
		source := executeTestSource{position: mgl64.Vec3{0, 64, 0}}
		out := &cmd.Output{}
		(dataCommand{Args: `merge block 0 64 0 {"CustomName":"Treasury"}`}).Run(source, out, tx)
		if out.ErrorCount() != 0 {
			t.Fatalf("data merge errors: %v", out.Errors())
		}
		chest, ok := tx.Block(pos).(block.Chest)
		if !ok || chest.CustomName != "Treasury" {
			t.Fatalf("merged chest name=%q, block type ok=%v", chest.CustomName, ok)
		}
		got := &cmd.Output{}
		(dataCommand{Args: "get block 0 64 0 CustomName"}).Run(source, got, tx)
		if got.ErrorCount() != 0 || got.MessageCount() != 1 || got.Messages()[0].String() != `"Treasury"` {
			t.Fatalf("data get output=%v errors=%v", got.Messages(), got.Errors())
		}
	})
}
