package builtin

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
)

func TestSetBlockFillAndCloneCommands(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	<-w.Exec(func(tx *world.Tx) {
		o := &cmd.Output{}
		(setBlockCommand{X: 1, Y: 64, Z: 1, Block: "minecraft:stone"}).Run(nil, o, tx)
		(fillCommand{X1: 2, Y1: 64, Z1: 1, X2: 3, Y2: 64, Z2: 1, Block: "minecraft:dirt"}).Run(nil, o, tx)
		(cloneCommand{X1: 1, Y1: 64, Z1: 1, X2: 3, Y2: 64, Z2: 1, DX: 8, DY: 64, DZ: 8}).Run(nil, o, tx)
		for _, tc := range []struct {
			pos  cube.Pos
			name string
		}{
			{cube.Pos{1, 64, 1}, "minecraft:stone"}, {cube.Pos{2, 64, 1}, "minecraft:dirt"}, {cube.Pos{3, 64, 1}, "minecraft:dirt"},
			{cube.Pos{8, 64, 8}, "minecraft:stone"}, {cube.Pos{9, 64, 8}, "minecraft:dirt"}, {cube.Pos{10, 64, 8}, "minecraft:dirt"},
		} {
			name, _ := tx.Block(tc.pos).EncodeBlock()
			if name != tc.name {
				t.Errorf("block at %v = %q, want %q", tc.pos, name, tc.name)
			}
		}
	})
}

func TestWeatherAndBorderCommands(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	<-w.Exec(func(tx *world.Tx) {
		o := &cmd.Output{}
		(weatherCommand{Mode: "thunder"}).Run(nil, o, tx)
		if !tx.Thundering() {
			t.Fatal("thunder command did not enable thunder")
		}
		(borderCommand{Action: "set", Values: "32"}).Run(nil, o, tx)
		if got := tx.World().Border().Size; got != 32 {
			t.Fatalf("border size=%v, want 32", got)
		}
		(borderCommand{Action: "add", Values: "8 20"}).Run(nil, o, tx)
		if got := tx.World().Border(); got.Size != 32 || got.LerpTarget != 40 || got.LerpTime != 400 {
			t.Fatalf("border add transition = %+v, want size/target/time 32/40/400", got)
		}
		(borderCommand{Action: "damage", Values: "amount 1.5"}).Run(nil, o, tx)
		(borderCommand{Action: "damage", Values: "buffer 7"}).Run(nil, o, tx)
		(borderCommand{Action: "warning", Values: "distance 12"}).Run(nil, o, tx)
		(borderCommand{Action: "warning", Values: "time 30"}).Run(nil, o, tx)
		got := tx.World().Border()
		if got.DamagePerBlock != 1.5 || got.SafeZone != 7 || got.WarningBlocks != 12 || got.WarningTime != 30 {
			t.Fatalf("border settings=%+v", got)
		}
	})
}

func TestSummonCommand(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: entity.DefaultRegistry}.New()
	defer w.Close()
	<-w.Exec(func(tx *world.Tx) {
		before := tx.World().EntityCount()
		(summonCommand{Entity: "minecraft:pig", X: 0.5, Y: 64, Z: 0.5}).Run(nil, &cmd.Output{}, tx)
		if got := tx.World().EntityCount(); got != before+1 {
			t.Fatalf("entity count=%d, want %d", got, before+1)
		}
	})
}
