package world_test

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/world"
)

// TestWorldCountsItsTicks checks that the Stats of a world count every
// tick, the one it runs as it is made included, and nothing of chunks or
// generation it did not do.
func TestWorldCountsItsTicks(t *testing.T) {
	t.Parallel()
	w := world.Config{Synchronous: true}.New()
	defer w.Close()
	s := w.Stats()
	before := s.Ticks.Count()
	if before != 1 {
		t.Errorf("%d ticks counted as the world was made, want 1", before)
	}
	for range 40 {
		w.AdvanceTick()
	}
	if got := s.Ticks.Count() - before; got != 40 {
		t.Errorf("%d ticks counted, want 40", got)
	}
	if s.Ticks.Sum() <= 0 {
		t.Error("ticks took no time")
	}
	if s.TicksSkipped.Load() != 0 || s.GenerationFailures.Load() != 0 || s.ChunkLoadErrors.Load() != 0 || w.GenerationQueueLength() != 0 {
		t.Error("a world that only ticked counted more")
	}
}

// TestWorldCountsWhatItsOwnerDoes checks that the Stats of a world count the
// time its owner spends running transactions, and that the transactions
// waiting for it are counted while it is busy.
func TestWorldCountsWhatItsOwnerDoes(t *testing.T) {
	t.Parallel()
	w := world.Config{}.New()
	defer w.Close()

	started, release := make(chan struct{}), make(chan struct{})
	before := w.Stats().OwnerBusy.Load()
	done := w.Exec(func(*world.Tx) {
		close(started)
		<-release
	})
	<-started
	var waiting []<-chan struct{}
	for range 3 {
		waiting = append(waiting, w.Exec(func(*world.Tx) {}))
	}
	if got := w.QueuedTransactions(); got < 3 {
		t.Errorf("%d transactions queued behind a busy owner, want at least 3", got)
	}
	time.Sleep(30 * time.Millisecond)
	close(release)
	<-done
	for _, c := range waiting {
		<-c
	}
	if busy := time.Duration(w.Stats().OwnerBusy.Load() - before); busy < 30*time.Millisecond {
		t.Errorf("owner counted busy for %v while a transaction ran for 30ms", busy)
	}
	if got := w.QueuedTransactions(); got > 1 {
		// Only a tick may be waiting by now.
		t.Errorf("%d transactions still queued after the owner ran them all", got)
	}
}
