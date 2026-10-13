package builtin

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func worldTestCommand(name string, runnables ...cmd.Runnable) cmd.Command {
	return cmd.New(name, "", nil, runnables...)
}

func TestSetBlockFillAndCloneCommands(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	<-w.Exec(func(tx *world.Tx) {
		setblock := worldTestCommand("setblock", setBlockCommand{})
		fill := worldTestCommand("fill", fillCommand{}, fillReplaceCommand{})
		clone := worldTestCommand("clone", cloneCommand{})
		runTestCommand(setblock, "1 64 1 minecraft:stone", tx)
		runTestCommand(fill, "2 64 1 3 64 1 minecraft:dirt", tx)
		if out := runTestCommand(clone, "1 64 1 3 64 1 8 64 8", tx); out.ErrorCount() != 0 {
			t.Fatalf("clone errors: %v", out.Errors())
		}
		for _, tc := range []struct {
			pos  cube.Pos
			name string
		}{
			{cube.Pos{1, 64, 1}, "minecraft:stone"}, {cube.Pos{2, 64, 1}, "minecraft:dirt"}, {cube.Pos{3, 64, 1}, "minecraft:dirt"},
			{cube.Pos{8, 64, 8}, "minecraft:stone"}, {cube.Pos{9, 64, 8}, "minecraft:dirt"}, {cube.Pos{10, 64, 8}, "minecraft:dirt"},
		} {
			if name, _ := tx.Block(tc.pos).EncodeBlock(); name != tc.name {
				t.Errorf("block at %v = %q, want %q", tc.pos, name, tc.name)
			}
		}
		if out := runTestCommand(clone, "1 64 1 3 64 1 2 64 1", tx); out.ErrorCount() == 0 {
			t.Fatal("normal clone accepted overlapping source and destination")
		}
		if out := runTestCommand(fill, "0 0 0 40 40 40 minecraft:stone", tx); out.ErrorCount() == 0 {
			t.Fatal("fill accepted a volume above the Java block limit")
		}
		runTestCommand(fill, "20 60 20 22 62 22 minecraft:stone hollow", tx)
		if name, _ := tx.Block(cube.Pos{21, 61, 21}).EncodeBlock(); name != "minecraft:air" {
			t.Fatalf("hollow fill interior = %s, want air", name)
		}
		if name, _ := tx.Block(cube.Pos{20, 61, 21}).EncodeBlock(); name != "minecraft:stone" {
			t.Fatalf("hollow fill shell = %s, want stone", name)
		}
		runTestCommand(fill, "20 60 20 22 62 22 minecraft:glass replace minecraft:stone", tx)
		if name, _ := tx.Block(cube.Pos{20, 61, 21}).EncodeBlock(); name != "minecraft:glass" {
			t.Fatalf("filtered replace = %s, want glass", name)
		}
	})
}

func TestRelativeAndLocalCoordinates(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	<-w.Exec(func(tx *world.Tx) {
		setblock := worldTestCommand("setblock", setBlockCommand{})
		out := &cmd.Output{}
		src := consoleTestSource{pos: mgl64.Vec3{10.5, 64, -5.5}, out: out}
		setblock.Execute("~ ~1 ~-2 minecraft:stone", src, tx)
		if name, _ := tx.Block(cube.Pos{10, 65, -8}).EncodeBlock(); name != "minecraft:stone" {
			t.Fatalf("relative setblock missed {10 65 -8}: %v", out.Errors())
		}
		// With zero rotation, ^ forwards is +Z and ^ left is +X, as in Java.
		ctx := cmd.WithContext(src, cmd.ExecutionContext{Position: mgl64.Vec3{0.5, 70, 0.5}})
		setblock.Execute("^1 ^ ^3 minecraft:dirt", ctx, tx)
		if name, _ := tx.Block(cube.Pos{1, 70, 3}).EncodeBlock(); name != "minecraft:dirt" {
			t.Fatalf("local setblock missed {1 70 3}: %v", out.Errors())
		}
		out = &cmd.Output{}
		setblock.Execute("^1 ~ ^3 minecraft:dirt", consoleTestSource{out: out}, tx)
		if out.ErrorCount() == 0 {
			t.Fatal("mixed local and world coordinates were accepted")
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
		runTestCommand(worldTestCommand("summon", summonCommand{}), "minecraft:pig 0.5 64 0.5", tx)
		if got := tx.World().EntityCount(); got != before+1 {
			t.Fatalf("entity count=%d, want %d", got, before+1)
		}
	})
}
