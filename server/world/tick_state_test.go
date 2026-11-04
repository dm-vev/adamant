package world_test

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/world"
)

// TestTickStateOfAStuckOwner checks the TickState of a world whose owner is
// held by a transaction: the tick asked for meanwhile runs from when it was
// asked for, until the owner is free and the tick ends; between ticks the
// world is idle.
func TestTickStateOfAStuckOwner(t *testing.T) {
	t.Parallel()
	w := world.Config{Generator: world.NopGenerator{}}.New()
	defer w.Close()
	if s := w.TickState(); !s.Ticking || s.Since.IsZero() {
		t.Fatalf("a new world: %+v, want ticking and made", s)
	}
	for deadline := time.Now().Add(5 * time.Second); w.TickState().Ended.IsZero(); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the tick loop ended no tick")
		}
	}

	release, held := make(chan struct{}), make(chan struct{})
	w.Do(func(*world.Tx) {
		close(held)
		<-release
	})
	<-held
	var stuck world.TickState
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if stuck = w.TickState(); stuck.Running {
			break
		}
		if time.Now().After(deadline) {
			close(release)
			t.Fatal("no tick ran while the owner was held")
		}
	}
	time.Sleep(200 * time.Millisecond)
	if s := w.TickState(); !s.Running || !s.Started.Equal(stuck.Started) {
		close(release)
		t.Fatalf("held owner: %+v, want running since %v", s, stuck.Started)
	}
	close(release)

	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(time.Millisecond) {
		s := w.TickState()
		if !s.Running && s.Ended.After(stuck.Started) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the tick did not end once the owner was free: %+v", s)
		}
	}

	sync := world.Config{Synchronous: true}.New()
	defer sync.Close()
	if sync.TickState().Ticking {
		t.Error("a Synchronous world ticks on its own")
	}
}
