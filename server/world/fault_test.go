package world

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
)

// faultRecorder is a slog.Handler keeping the records logged through it.
type faultRecorder struct {
	mu      *sync.Mutex
	records *[]slog.Record
}

func newFaultRecorder() faultRecorder {
	return faultRecorder{mu: new(sync.Mutex), records: new([]slog.Record)}
}

func (h faultRecorder) Enabled(context.Context, slog.Level) bool { return true }
func (h faultRecorder) WithAttrs([]slog.Attr) slog.Handler       { return h }
func (h faultRecorder) WithGroup(string) slog.Handler            { return h }
func (h faultRecorder) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	*h.records = append(*h.records, r.Clone())
	h.mu.Unlock()
	return nil
}

// errors returns the records logged at Error.
func (h faultRecorder) errors() []slog.Record {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []slog.Record
	for _, r := range *h.records {
		if r.Level == slog.LevelError {
			out = append(out, r)
		}
	}
	return out
}

// attr returns the value of the attribute key of r as a string.
func attr(r slog.Record, key string) string {
	var v string
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == key {
			v = a.Value.String()
			return false
		}
		return true
	})
	return v
}

// fakeClock is a clock tests move by hand.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time { return c.t }

// faultTestWorld returns a world logging to rec, with its fault log on the
// clock c.
func faultTestWorld(t *testing.T, rec faultRecorder, c *fakeClock, conf Config) *World {
	t.Helper()
	conf.Log = slog.New(rec)
	conf.Synchronous = true
	w := conf.New()
	t.Cleanup(func() { _ = w.Close() })
	if c != nil {
		w.faults.now = c.now
	}
	return w
}

func TestFaultLogIsRateLimited(t *testing.T) {
	rec := newFaultRecorder()
	clock := &fakeClock{t: time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)}
	var hooked int
	w := faultTestWorld(t, rec, clock, Config{Fault: func(Fault) { hooked++ }})

	// 1,000 panics of one key, one every 100 ms: 100 seconds.
	for i := range 1000 {
		f := Fault{Kind: FaultEntity, Type: "test:zombie", Pos: cube.Pos{i, 64, 0}, Value: "boom", Stack: []byte("stack")}
		w.reportFault(f, quarantineKey{})
		clock.t = clock.t.Add(100 * time.Millisecond)
	}
	lines := rec.errors()
	if len(lines) != 2 {
		t.Fatalf("lines after 1000 panics of one key = %d, want 2 (first, then one summary a minute later)", len(lines))
	}
	if lines[0].Message != "entity panicked" || attr(lines[0], "stack") != "stack" || attr(lines[0], "type") != "test:zombie" {
		t.Fatalf("first line = %q %v, want the panic with its stack and type", lines[0].Message, lines[0])
	}
	if want := "599 more panics of entity test:zombie since 12:00:00"; lines[1].Message != want {
		t.Fatalf("summary = %q, want %q", lines[1].Message, want)
	}
	if attr(lines[1], "stack") != "stack" || attr(lines[1], "pos") != "(600,64,0)" {
		t.Fatalf("summary does not carry the panic that ended the quiet minute: %v", lines[1])
	}
	if hooked != 1000 {
		t.Fatalf("Config.Fault called %d times, want 1000: it is not rate-limited", hooked)
	}

	// Another key logs at once.
	w.reportFault(Fault{Kind: FaultEntity, Type: "test:cow", Value: "boom"}, quarantineKey{})
	if lines = rec.errors(); len(lines) != 3 || lines[2].Message != "entity panicked" || attr(lines[2], "type") != "test:cow" {
		t.Fatalf("another key did not log at once: %d lines", len(lines))
	}
	// The same type of another kind is another key.
	w.reportFault(Fault{Kind: FaultBlockEntity, Type: "test:cow", Value: "boom"}, quarantineKey{})
	if lines = rec.errors(); len(lines) != 4 || lines[3].Message != "block entity panicked" {
		t.Fatalf("another kind did not log at once: %d lines", len(lines))
	}
	// A minute after the summary, a key with no panic suppressed logs as
	// a first panic again.
	clock.t = clock.t.Add(2 * time.Minute)
	w.reportFault(Fault{Kind: FaultEntity, Type: "test:cow", Value: "boom"}, quarantineKey{})
	if lines = rec.errors(); len(lines) != 5 || lines[4].Message != "entity panicked" {
		t.Fatalf("a quiet key did not log as a first panic: %v", lines[len(lines)-1].Message)
	}
}

func TestFaultLogEvictsTheOldestKey(t *testing.T) {
	rec := newFaultRecorder()
	clock := &fakeClock{t: time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)}
	w := faultTestWorld(t, rec, clock, Config{})

	for i := range faultLogKeys {
		w.reportFault(Fault{Kind: FaultScheduledTick, Type: fmt.Sprintf("test:block_%d", i)}, quarantineKey{})
		clock.t = clock.t.Add(time.Millisecond)
	}
	// The oldest key panics twice more, unlogged.
	w.reportFault(Fault{Kind: FaultScheduledTick, Type: "test:block_0"}, quarantineKey{})
	w.reportFault(Fault{Kind: FaultScheduledTick, Type: "test:block_0"}, quarantineKey{})
	if n := len(rec.errors()); n != faultLogKeys {
		t.Fatalf("lines = %d, want %d", n, faultLogKeys)
	}
	// A new key evicts the oldest, summarising what it did not log.
	w.reportFault(Fault{Kind: FaultScheduledTick, Type: "test:new"}, quarantineKey{})
	lines := rec.errors()
	if len(lines) != faultLogKeys+2 {
		t.Fatalf("lines = %d, want %d: the summary of the evicted key and the new one", len(lines), faultLogKeys+2)
	}
	if want := "2 more panics of scheduled tick test:block_0 since 12:00:00"; lines[faultLogKeys].Message != want {
		t.Fatalf("eviction summary = %q, want %q", lines[faultLogKeys].Message, want)
	}
	if len(w.faults.entries) != faultLogKeys {
		t.Fatalf("log keeps %d keys, want at most %d", len(w.faults.entries), faultLogKeys)
	}
	// The evicted key is a new key again.
	w.reportFault(Fault{Kind: FaultScheduledTick, Type: "test:block_0"}, quarantineKey{})
	if lines = rec.errors(); lines[len(lines)-1].Message != "scheduled tick panicked" {
		t.Fatalf("evicted key did not log as a first panic: %q", lines[len(lines)-1].Message)
	}
}

func TestFaultCountQuarantinesAtTheLimit(t *testing.T) {
	var c faultCount
	for i, tick := range []int64{100, 200, 300} {
		var f Fault
		quarantine := c.count(&f, tick)
		if f.Count != i+1 || quarantine != (i+1 >= FaultLimit) || f.Quarantined != quarantine {
			t.Fatalf("panic %d at tick %d: count=%d quarantined=%v", i+1, tick, f.Count, f.Quarantined)
		}
	}

	// Panics further apart than the window start the count again.
	c = faultCount{}
	c.add(0)
	c.add(FaultWindow - 1)
	if n := c.add(FaultWindow); n != 1 {
		t.Fatalf("count FaultWindow ticks after the first panic = %d, want 1", n)
	}
	if n := c.add(FaultWindow + 10); n != 2 {
		t.Fatalf("count within the new window = %d, want 2", n)
	}
	// A clock going back, as a world whose time was set, starts it again.
	if n := c.add(5); n != 1 {
		t.Fatalf("count after the tick went back = %d, want 1", n)
	}
	// The count saturates.
	c = faultCount{}
	for range 300 {
		c.add(1)
	}
	if c.n != 255 {
		t.Fatalf("count = %d, want 255", c.n)
	}
}

func TestQuarantinedListsAndReleases(t *testing.T) {
	rec := newFaultRecorder()
	w := faultTestWorld(t, rec, nil, Config{})
	h := NewEntity(taskTestEntityType{}, taskTestEntityConfig{})

	entity := Fault{Kind: FaultEntity, Type: "test:entity", Entity: h, Count: 3, Quarantined: true}
	actor := Fault{Kind: FaultBlockEntity, Type: "test:furnace", Pos: cube.Pos{1, 2, 3}, Count: 3, Quarantined: true}
	block := Fault{Kind: FaultScheduledTick, Type: "test:block", Pos: cube.Pos{40, 2, 3}, Count: 3, Quarantined: true}
	w.reportFault(Fault{Kind: FaultEntity, Type: "test:entity", Entity: h, Count: 1}, quarantineKey{entity: h})
	if q := w.Quarantined(); len(q) != 0 {
		t.Fatalf("a fault below the limit quarantined: %v", q)
	}
	w.reportFault(entity, quarantineKey{entity: h})
	w.reportFault(actor, quarantineKey{pos: actor.Pos, blockEntity: true})
	w.reportFault(block, quarantineKey{pos: block.Pos, name: block.Type})
	q := w.Quarantined()
	if len(q) != 3 || q[0].Entity != h || q[1].Type != "test:furnace" || q[2].Type != "test:block" || q[0].World != w {
		t.Fatalf("Quarantined = %v, want the entity, the block entity and the block, in kind order", q)
	}
	if !strings.Contains(attr(rec.errors()[1], "quarantined"), "true") {
		t.Fatalf("the quarantining fault does not say so in the log")
	}

	// Unloading the chunk of the block entity releases it and not the rest.
	w.releaseChunk(ChunkPos{0, 0})
	if q = w.Quarantined(); len(q) != 2 || q[0].Entity != h || q[1].Type != "test:block" {
		t.Fatalf("Quarantined after unloading chunk 0,0 = %v", q)
	}
	w.release(quarantineKey{entity: h})
	w.release(quarantineKey{pos: block.Pos, name: block.Type})
	if q = w.Quarantined(); len(q) != 0 {
		t.Fatalf("Quarantined after release = %v", q)
	}
}

func TestFatalCallsTheHookOnce(t *testing.T) {
	rec := newFaultRecorder()
	var fatal []Fault
	w := faultTestWorld(t, rec, nil, Config{Fatal: func(f Fault) { fatal = append(fatal, f) }})
	w.fatal(Fault{Type: "test glue", Value: "first", Stack: []byte("stack")})
	w.fatal(Fault{Type: "test glue", Value: "second"})
	if len(fatal) != 1 || fatal[0].Value != "first" || fatal[0].Kind != FaultFatal || fatal[0].World != w {
		t.Fatalf("Config.Fatal calls = %v, want the first fault only, of kind fatal", fatal)
	}
	if lines := rec.errors(); len(lines) != 2 || lines[0].Message != "world failed" || attr(lines[0], "stack") != "stack" {
		t.Fatalf("fatal faults are not all logged with their stack: %d lines", len(lines))
	}
}

func TestFaultKindNames(t *testing.T) {
	for k := FaultEntity; k <= FaultFatal; k++ {
		if s := k.String(); strings.HasPrefix(s, "FaultKind(") {
			t.Fatalf("kind %d has no name", k)
		}
	}
	if s := FaultKind(200).String(); s != "FaultKind(200)" {
		t.Fatalf("unknown kind = %q", s)
	}
}

func TestBlockFaultsForgetTheOldest(t *testing.T) {
	rec := newFaultRecorder()
	w := faultTestWorld(t, rec, nil, Config{})
	at := func(i int) cube.Pos { return cube.Pos{i % 64, 10, i / 64} }

	// The first block is quarantined, then blockFaultEntries others panic.
	for range FaultLimit {
		w.blockPanicked(FaultScheduledTick, 1, at(0), "test:first", "boom")
	}
	if q := w.Quarantined(); len(q) != 1 || q[0].Pos != at(0) {
		t.Fatalf("Quarantined = %v, want the first block", q)
	}
	for i := 1; i <= blockFaultEntries; i++ {
		w.blockPanicked(FaultScheduledTick, 1, at(i), "test:other", "boom")
	}
	if n := len(w.blockFaults.m); n != blockFaultEntries {
		t.Fatalf("blocks kept = %d, want %d", n, blockFaultEntries)
	}
	if _, ok := w.blockFaults.m[at(0)]; ok || len(w.Quarantined()) != 0 {
		t.Fatal("the block whose last panic is the oldest was not forgotten, or is still quarantined")
	}
	// Another block at a position is another offender.
	w.blockPanicked(FaultScheduledTick, 1, at(1), "test:third", "boom")
	if e := w.blockFaults.m[at(1)]; e.name != "test:third" || e.faults.n != 1 {
		t.Fatalf("entry at a position another block panicked at = %+v", e)
	}
	// Unloading a chunk forgets its blocks.
	w.forgetChunk(ChunkPos{0, 0})
	for p := range w.blockFaults.m {
		if chunkPosFromBlockPos(p) == (ChunkPos{0, 0}) {
			t.Fatalf("block %v of an unloaded chunk kept", p)
		}
	}
}
