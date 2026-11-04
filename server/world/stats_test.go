package world_test

import (
	"testing"

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
