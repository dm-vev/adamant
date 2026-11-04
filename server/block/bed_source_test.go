package block

import (
	"log/slog"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// A straw bed checks the blocks above it for suffocation. Blocks without a
// Java block state are read by their collision shape, as Java's default
// suffocation reads one, instead of panicking: a full cube suffocates, the
// rest does not, and a block holding water Java cannot waterlog it with is
// read as the block alone.
func TestStrawBedObstructionReadsBlocksWithoutJavaState(t *testing.T) {
	native := func(name string, properties map[string]any) world.Block {
		b, ok := world.BlockByName(name, properties)
		if !ok {
			t.Fatalf("no native block %s%v", name, properties)
		}
		if _, err := world.CanonicalStateOf(b); err == nil {
			t.Fatalf("%s%v has a Java block state", name, properties)
		}
		return b
	}
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Log: slog.New(slog.DiscardHandler)}.New()
	t.Cleanup(func() { _ = w.Close() })
	done := false
	<-w.Exec(func(tx *world.Tx) {
		for i, tc := range []struct {
			name      string
			b         world.Block
			water     bool
			suffocate bool
		}{
			{name: "jigsaw of a rotation Java has not", b: native("minecraft:jigsaw", map[string]any{"facing_direction": int32(2), "rotation": int32(1)}), suffocate: true},
			{name: "leaf litter of five segments", b: native("minecraft:leaf_litter", map[string]any{"growth": int32(5), "minecraft:cardinal_direction": "east"})},
			{name: "ceiling skull", b: native("minecraft:skeleton_skull", map[string]any{"facing_direction": int32(0)})},
			{name: "carpet holding water", b: Carpet{}, water: true},
		} {
			pos := cube.Pos{i * 2, 70, 0}
			tx.SetBlock(pos, tc.b, nil)
			if tc.water {
				tx.SetLiquid(pos, Water{Still: true, Depth: 8})
				if _, err := tx.BlockStateID(pos); err == nil {
					t.Fatalf("%v: has a Java block state", tc.name)
				}
			}
			if got := bedBlockSuffocates(pos, tx); got != tc.suffocate {
				t.Errorf("%v: suffocates = %v, want %v", tc.name, got, tc.suffocate)
			}
		}
		done = true
	})
	if !done {
		t.Fatal("the transaction panicked")
	}
}
