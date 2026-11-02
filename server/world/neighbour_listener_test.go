package world

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

type immediateNeighbourTestBlock struct {
	neighbourMechanicsTestBlock
	observed *int
}

func (b *immediateNeighbourTestBlock) NeighbourChanged(cube.Pos, cube.Pos, *Tx) {
	*b.observed++
}

func TestImmediateNeighbourListenerRejectsOldOccupantNotifications(t *testing.T) {
	for _, generating := range []bool{false, true} {
		t.Run(map[bool]string{false: "replacement", true: "generating"}[generating], func(t *testing.T) {
			var calls int
			var legacy []string
			listener := &immediateNeighbourTestBlock{neighbourMechanicsTestBlock: neighbourMechanicsTestBlock{name: "bubble", updates: &legacy}, observed: &calls}
			old := &neighbourMechanicsTestBlock{name: "displacer", updates: &legacy}
			registry := NewBlockRegistry()
			for _, b := range []Block{listener, old} {
				name, properties := b.EncodeBlock()
				registry.RegisterBlockState(BlockState{Name: name, Properties: properties})
				registry.RegisterBlock(b)
			}
			w := Config{Blocks: registry, Synchronous: true, RandomTickSpeed: -1}.New()
			t.Cleanup(func() { _ = w.Close() })
			pos, changed := cube.Pos{16, 64, 0}, cube.Pos{15, 64, 0}
			if generating {
				col := newColumn(chunk.New(registry, w.Range()))
				col.SetBlock(0, 64, 0, 0, registry.BlockRuntimeID(listener))
				w.chunks[ChunkPos{1, 0}] = col
				w.updateNeighbour(pos, changed)
				w.AdvanceTick()
				if calls != 0 || len(legacy) != 0 {
					t.Fatal("generating chunk received notification")
				}
				col.fillLight(ChunkPos{1, 0})
				col.markReady()
			} else {
				w.Do(func(tx *Tx) {
					opts := &SetOpts{DisableBlockUpdates: true}
					tx.SetBlock(pos, old, opts)
					tx.updateNeighbour(pos, changed)
					tx.SetBlock(pos, listener, opts)
				})
			}
			w.AdvanceTick()
			want := 0
			if generating {
				want = 1
			}
			if calls != want || len(legacy) != 0 {
				t.Fatalf("callbacks immediate=%d legacy=%v, want immediate=%d", calls, legacy, want)
			}
			w.Do(func(tx *Tx) { tx.updateNeighbour(pos, changed) })
			if calls != want+1 {
				t.Fatal("loaded listener missed synchronous notification")
			}
			w.AdvanceTick()
			if calls != want+1 || len(legacy) != 0 {
				t.Fatal("synchronous listener received a duplicate callback")
			}
		})
	}
}
