package builtin

import (
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

func TestBlockCompletionsUseSourceRegistry(t *testing.T) {
	registry := world.NewBlockRegistry()
	for _, variant := range []int32{0, 1} {
		registry.RegisterBlockState(world.BlockState{Name: "example:completion_block", Properties: map[string]any{"variant": variant}})
	}
	w := world.Config{Blocks: registry, Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		check := func(src cmd.Source) {
			t.Helper()
			values := (blockArgument("")).Options(src)
			if count := len(slices.DeleteFunc(slices.Clone(values), func(value string) bool { return value != "example:completion_block" })); count != 1 {
				t.Fatalf("custom block completion appears %d times, want once", count)
			}
			if slices.Contains(values, "completion_block") {
				t.Fatal("non-minecraft namespace was dropped from completion")
			}
		}
		check(registryParameterSource{tx: tx})
		check(cmd.WithContext(registryParameterSource{tx: tx}, cmd.ExecutionContext{}))
		check(cmd.WithContext(consoleTestSource{}, cmd.ExecutionContext{World: w}))
		if slices.Contains((blockArgument("")).Options(nil), "example:completion_block") {
			t.Fatal("custom block leaked into another registry's completions")
		}
	})
}

func TestItemCompletionsKeepCommandExecution(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		p := tx.AddEntity(world.EntitySpawnOpts{}.New(player.Type, player.Config{Name: "RegistryTarget", GameMode: world.GameModeCreative})).(*player.Player)
		if out := runTestCommand(newGiveCommand(), "@a minecraft:diamond 2", tx); out.ErrorCount() != 0 {
			t.Fatalf("give errors: %v", out.Errors())
		}
		if got, _ := p.Inventory().Item(0); got.Count() != 2 || !got.Comparable(item.NewStack(item.Diamond{}, 1)) {
			t.Fatalf("give inventory = %v", got)
		}
		if out := runTestCommand(newItemCommand(), "@a 0 diamond 3", tx); out.ErrorCount() != 0 {
			t.Fatalf("item errors: %v", out.Errors())
		}
		if got, _ := p.Inventory().Item(0); got.Count() != 3 {
			t.Fatalf("item count = %d, want 3", got.Count())
		}
		for _, javaAlias := range []string{"minecraft:oak_button", "red_bed"} {
			if out := runTestCommand(newGiveCommand(), "@a "+javaAlias, tx); out.ErrorCount() != 0 {
				t.Errorf("give Java alias %s errors: %v", javaAlias, out.Errors())
			}
		}
	})
}

type registryParameterSource struct {
	cmd.Source
	tx *world.Tx
}

func (s registryParameterSource) Tx() *world.Tx { return s.tx }
