package world

import (
	"math/rand/v2"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
)

type cancelBatchTestBlock struct {
	name string
	id   uint64
	tick func(cube.Pos, *Tx)
}

func (b cancelBatchTestBlock) EncodeBlock() (string, map[string]any)            { return b.name, nil }
func (b cancelBatchTestBlock) Hash() (uint64, uint64)                           { return 1 << 50, b.id }
func (cancelBatchTestBlock) Model() BlockModel                                  { return redstoneCancellationModel{} }
func (b cancelBatchTestBlock) ScheduledTick(pos cube.Pos, tx *Tx, _ *rand.Rand) { b.tick(pos, tx) }

func TestScheduledTickCancelCollectedCallbackAndReschedule(t *testing.T) {
	registry := NewBlockRegistry()
	aPos, bPos, cPos := cube.Pos{0, 65, 0}, cube.Pos{1, 65, 0}, cube.Pos{2, 65, 0}
	var aCalls, bCalls, cCalls int
	b := cancelBatchTestBlock{name: "test:cancel_batch_b", id: 2, tick: func(cube.Pos, *Tx) { bCalls++ }}
	c := cancelBatchTestBlock{name: "test:cancel_batch_c", id: 3, tick: func(cube.Pos, *Tx) { cCalls++ }}
	a := cancelBatchTestBlock{name: "test:cancel_batch_a", id: 1, tick: func(_ cube.Pos, tx *Tx) {
		aCalls++
		tx.CancelBlockUpdate(bPos, b)
		// The already-collected b callback must be cancelled without
		// cancelling this new tick scheduled for the following batch.
		tx.ScheduleBlockUpdate(bPos, b, time.Second/20)
	}}
	for _, b := range []cancelBatchTestBlock{a, b, c} {
		registry.RegisterBlockState(BlockState{Name: b.name, Properties: map[string]any{}})
		registry.RegisterBlock(b)
	}
	w := Config{Synchronous: true, Blocks: registry}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *Tx) {
		for i, b := range []cancelBatchTestBlock{a, b, c} {
			pos := []cube.Pos{aPos, bPos, cPos}[i]
			tx.SetBlock(pos, b, nil)
			tx.ScheduleBlockUpdate(pos, b, time.Second/20)
		}
	})
	w.AdvanceTick()
	if aCalls != 1 || bCalls != 0 || cCalls != 1 {
		t.Fatalf("first batch callbacks: a=%d b=%d c=%d", aCalls, bCalls, cCalls)
	}
	w.AdvanceTick()
	if aCalls != 1 || bCalls != 1 || cCalls != 1 {
		t.Fatalf("rescheduled batch callbacks: a=%d b=%d c=%d", aCalls, bCalls, cCalls)
	}
}
