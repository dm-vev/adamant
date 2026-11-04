package world

import (
	"strings"
	"testing"
	"time"
)

func TestWatchdogLogsAStuckTickOnce(t *testing.T) {
	rec := newFaultRecorder()
	clock := &fakeClock{t: time.Unix(1_000_000, 0)}
	w := faultTestWorld(t, rec, nil, Config{})
	d := watchdog{w: w, now: clock.now}

	if d.check() {
		t.Fatal("reported a world not ticking")
	}
	start := clock.now()
	w.tickStarted.Store(start.UnixNano())
	clock.t = clock.t.Add(watchdogTimeout - time.Second)
	if d.check() {
		t.Fatal("reported a tick running for less than the timeout")
	}
	clock.t = clock.t.Add(time.Second)
	if !d.check() {
		t.Fatal("did not report a tick running for the timeout")
	}
	lines := rec.errors()
	if len(lines) != 1 || !strings.Contains(attr(lines[0], "stack"), "goroutine ") || !strings.Contains(attr(lines[0], "stack"), "TestWatchdogLogsAStuckTickOnce") || attr(lines[0], "running") != "1m0s" {
		t.Fatalf("Error lines = %v, want one with the stacks of the goroutines", lines)
	}
	// The same tick is reported once, however long it runs.
	clock.t = clock.t.Add(10 * watchdogTimeout)
	if d.check() || len(rec.errors()) != 1 {
		t.Fatal("reported the same tick again")
	}
	// The next tick that runs too long is reported again.
	w.tickStarted.Store(0)
	if d.check() {
		t.Fatal("reported a world between ticks")
	}
	w.tickStarted.Store(clock.now().UnixNano())
	clock.t = clock.t.Add(watchdogTimeout)
	if !d.check() || len(rec.errors()) != 2 {
		t.Fatal("did not report the next tick running too long")
	}
}

func TestTickLoopMarksTheTickItWaitsFor(t *testing.T) {
	w := Config{Provider: NopProvider{}, RandomTickSpeed: -1}.New()
	defer func() { _ = w.Close() }()
	// The owner is held by a transaction: the tick queued after it waits,
	// marked as begun, as one stuck behind a frozen owner would be.
	release := make(chan struct{})
	held := w.Exec(func(*Tx) { <-release })
	deadline := time.Now().Add(10 * time.Second)
	for w.tickStarted.Load() == 0 {
		if time.Now().After(deadline) {
			close(release)
			t.Fatal("the tick loop did not mark the tick it waits for")
		}
		time.Sleep(time.Millisecond)
	}
	close(release)
	<-held
	deadline = time.Now().Add(10 * time.Second)
	for started := w.tickStarted.Load(); started != 0 && w.tickStarted.Load() == started; {
		if time.Now().After(deadline) {
			t.Fatal("the tick loop did not unmark the tick once it ended")
		}
		time.Sleep(time.Millisecond)
	}
}
