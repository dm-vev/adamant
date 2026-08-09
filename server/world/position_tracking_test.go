package world

import (
	"sync"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
)

func TestPositionTrackingDimensionAndWorldIdentity(t *testing.T) {
	shared := &Settings{}
	overworld := Config{Provider: NopProvider{Set: shared}, Dim: Overworld, Synchronous: true}.New()
	nether := Config{Provider: NopProvider{Set: shared}, Dim: Nether, Synchronous: true}.New()
	other := Config{Provider: NopProvider{Set: &Settings{}}, Dim: Overworld, Synchronous: true}.New()
	t.Cleanup(func() {
		_ = overworld.Close()
		_ = nether.Close()
		_ = other.Close()
	})

	pos := cube.Pos{3, 70, -4}
	overworldHandle := overworld.TrackPosition(pos, 0)
	netherHandle := nether.TrackPosition(pos, 0)
	if overworldHandle == 0 || netherHandle == 0 || overworldHandle == netherHandle {
		t.Fatalf("dimension handles = %d, %d", overworldHandle, netherHandle)
	}
	if got, dim, ok := overworld.TrackedPosition(netherHandle); !ok || got != pos || dim != 1 {
		t.Fatalf("shared nether target = %v, %d, %v", got, dim, ok)
	}
	if _, _, ok := other.TrackedPosition(overworldHandle); ok {
		t.Fatal("tracking handle leaked into an unrelated world")
	}
}

func TestPositionTrackingBreakAndReplacement(t *testing.T) {
	w := Config{Synchronous: true}.New()
	defer w.Close()
	pos := cube.Pos{8, 64, 8}
	handle := w.TrackPosition(pos, 0)
	w.UntrackPosition(pos)
	if _, _, ok := w.TrackedPosition(handle); ok {
		t.Fatal("untracked position remained active")
	}
	if got := w.TrackPosition(pos, 0); got != handle {
		t.Fatalf("replacement handle = %d, want %d", got, handle)
	}
}

func TestPositionTrackingCleanupIsBounded(t *testing.T) {
	settings := &Settings{}
	w := Config{Provider: NopProvider{Set: settings}, Synchronous: true}.New()
	defer w.Close()
	for x := range maxInactivePositionTrackingEntries + 32 {
		pos := cube.Pos{x, 64, 0}
		w.TrackPosition(pos, 0)
		w.UntrackPosition(pos)
	}
	data := settings.PositionTrackingData()
	if got := len(data.Entries); got != maxInactivePositionTrackingEntries {
		t.Fatalf("retained inactive entries = %d, want %d", got, maxInactivePositionTrackingEntries)
	}
	if got := w.PositionTrackingHandleAt(cube.Pos{0, 64, 0}); got != 0 {
		t.Fatalf("oldest evicted position still has handle %d", got)
	}
}

func TestPositionTrackingConcurrentWorldClose(t *testing.T) {
	settings := &Settings{}
	first := Config{Provider: NopProvider{Set: settings}, Dim: Overworld, Synchronous: true}.New()
	second := Config{Provider: NopProvider{Set: settings}, Dim: Nether, Synchronous: true}.New()
	first.TrackPosition(cube.Pos{1, 64, 1}, 0)
	second.TrackPosition(cube.Pos{1, 64, 1}, 0)

	done := make(chan struct{})
	go func() {
		defer close(done)
		var wg sync.WaitGroup
		for _, w := range []*World{first, second} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = w.Close()
			}()
		}
		for range 100 {
			_ = settings.PositionTrackingData()
		}
		wg.Wait()
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("concurrent world close deadlocked")
	}
}
