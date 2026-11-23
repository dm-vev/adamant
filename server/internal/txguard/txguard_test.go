package txguard

import (
	"testing"

	"github.com/df-mc/dragonfly/server/world"
)

func TestGuardNilAndClosedTx(t *testing.T) {
	t.Parallel()

	called := false
	if Run(nil, func() { called = true }) {
		t.Fatal("Run(nil) returned true")
	}
	if called {
		t.Fatal("Run(nil) invoked callback")
	}
	if v, ok := Value(nil, func() int { return 42 }); ok || v != 0 {
		t.Fatalf("Value(nil) = (%d, %v), want (0, false)", v, ok)
	}

	w := world.Config{RandomTickSpeed: -1}.New()
	defer func() { _ = w.Close() }()

	var captured *world.Tx
	<-w.Exec(func(tx *world.Tx) {
		captured = tx
		if !Run(tx, func() {
			called = true
		}) {
			t.Fatal("Run(open) returned false")
		}
		if v, ok := Value(tx, func() int { return 99 }); !ok || v != 99 {
			t.Fatalf("Value(open) = (%d, %v), want (99, true)", v, ok)
		}
	})
	if !called {
		t.Fatal("Run(open) did not run callback")
	}
	if !captured.Closed() {
		t.Fatal("captured tx should be closed after Exec finishes")
	}
	if Run(captured, func() { t.Fatal("closed tx should not run callback") }) {
		t.Fatal("Run(closed) returned true")
	}
	if v, ok := Value(captured, func() int {
		t.Fatal("closed tx should not run Value callback")
		return 1
	}); ok || v != 0 {
		t.Fatalf("Value(closed) = (%d, %v), want (0, false)", v, ok)
	}
}

func TestGuardDoesNotSwallowUserPanics(t *testing.T) {
	t.Parallel()

	w := world.Config{RandomTickSpeed: -1}.New()
	defer func() { _ = w.Close() }()

	done := make(chan any, 1)
	<-w.Exec(func(tx *world.Tx) {
		defer func() {
			done <- recover()
		}()
		Run(tx, func() {
			panic("boom")
		})
	})
	if got := <-done; got != "boom" {
		t.Fatalf("recovered panic = %v, want boom", got)
	}
}
