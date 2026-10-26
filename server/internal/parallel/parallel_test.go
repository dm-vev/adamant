package parallel

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// TestForVisitsEveryItemOnce checks every index is handed out exactly once
// for loop sizes around the batch boundaries.
func TestForVisitsEveryItemOnce(t *testing.T) {
	for _, n := range []int{0, 1, 7, 8, 9, 63, 64, 65, 1000, 4097} {
		counts := make([]atomic.Int32, n)
		For(n, 8, func(i int) { counts[i].Add(1) })
		for i := range counts {
			if c := counts[i].Load(); c != 1 {
				t.Fatalf("n=%d: item %d visited %d times", n, i, c)
			}
		}
	}
}

// TestForRaisesWorkerPanicAfterAllStop checks a panic on any worker reaches
// the caller as a *Panic and that no worker is still running afterwards.
func TestForRaisesWorkerPanicAfterAllStop(t *testing.T) {
	var running atomic.Int32
	cause := errors.New("boom")
	func() {
		defer func() {
			r := recover()
			p, ok := r.(*Panic)
			if !ok {
				t.Fatalf("recovered %T %v, want *Panic", r, r)
			}
			if !errors.Is(p, cause) || len(p.Stack) == 0 {
				t.Fatalf("panic %v does not carry the cause and stack", p)
			}
		}()
		For(1000, 1, func(i int) {
			running.Add(1)
			defer running.Add(-1)
			if i == 500 {
				panic(cause)
			}
			time.Sleep(10 * time.Microsecond)
		})
	}()
	if n := running.Load(); n != 0 {
		t.Fatalf("%d workers still running after For returned", n)
	}
}

// TestForNestedDoesNotDeadlock checks loops started from inside a loop
// finish, as nested parallel work may happen.
func TestForNestedDoesNotDeadlock(t *testing.T) {
	done := make(chan struct{})
	var sum atomic.Int64
	go func() {
		defer close(done)
		For(64, 1, func(int) {
			For(64, 1, func(int) { sum.Add(1) })
		})
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("nested For deadlocked")
	}
	if sum.Load() != 64*64 {
		t.Fatalf("nested loops ran %d items, want %d", sum.Load(), 64*64)
	}
}
