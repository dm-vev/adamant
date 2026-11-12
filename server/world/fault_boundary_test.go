package world

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/internal/faultcheck"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/go-gl/mathgl/mgl64"
)

// isolatedEnv names the test a process runs the body of, for isolated.
const isolatedEnv = "WORLD_ISOLATED_TEST"

// isolated runs body in a process of its own: a panic the world does not
// contain ends the process, or freezes a world owner, which in the test
// binary would take every other test with it. The parent fails with the
// end of the output of the child unless the child passed.
func isolated(t *testing.T, body func(t *testing.T)) {
	t.Helper()
	if os.Getenv(isolatedEnv) == t.Name() {
		body(t)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^"+t.Name()+"$", "-test.count=1", "-test.v")
	cmd.Env = append(os.Environ(), isolatedEnv+"="+t.Name())
	out, err := cmd.CombinedOutput()
	if err != nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		t.Fatalf("the world did not survive: %v\n%s", err, strings.Join(lines[max(0, len(lines)-40):], "\n"))
	}
}

// faultSink keeps the faults a world passes to Config.Fault and Fatal.
type faultSink struct {
	mu     sync.Mutex
	faults []Fault
	fatal  []Fault
}

func (s *faultSink) fault(f Fault) {
	s.mu.Lock()
	s.faults = append(s.faults, f)
	s.mu.Unlock()
}

func (s *faultSink) fatalFault(f Fault) {
	s.mu.Lock()
	s.fatal = append(s.fatal, f)
	s.mu.Unlock()
}

// all returns the faults of kind kept, or all of them for kind 0.
func (s *faultSink) all(kind FaultKind) []Fault {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Fault
	for _, f := range s.faults {
		if kind == 0 || f.Kind == kind {
			out = append(out, f)
		}
	}
	return out
}

func (s *faultSink) fatals() []Fault {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Fault(nil), s.fatal...)
}

// configure sets conf to log to rec and report to s.
func (s *faultSink) configure(conf Config, rec faultRecorder) Config {
	conf.Log = slog.New(rec)
	conf.Fault = s.fault
	conf.Fatal = s.fatalFault
	return conf
}

// within waits for c to close. After d, it prints the stack of the world
// owners and ends the process: closing the world would wait for a stuck
// owner forever.
func within(t *testing.T, d time.Duration, c <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-c:
	case <-time.After(d):
		buf := make([]byte, 1<<20)
		buf = buf[:runtime.Stack(buf, true)]
		for _, g := range strings.Split(string(buf), "\n\n") {
			if strings.Contains(g, "handleTransactions") {
				fmt.Fprintf(os.Stderr, "%s\n\n", g)
			}
		}
		fmt.Fprintf(os.Stderr, "%s within %v: the world owner is stuck\n", what, d)
		os.Exit(3)
	}
}

func TestExecPanicIsLogged(t *testing.T) {
	isolated(t, func(t *testing.T) {
		var sink faultSink
		rec := newFaultRecorder()
		w := sink.configure(Config{Provider: NopProvider{}, RandomTickSpeed: -1}, rec).New()
		defer func() { _ = w.Close() }()

		deferred := make(chan struct{})
		done := w.Exec(func(tx *Tx) {
			tx.Defer(func(*Tx) { close(deferred) })
			panic(faultcheck.Expected{Why: "test: exec panic"})
		})
		within(t, 5*time.Second, done, "the panicking Exec did not end")
		within(t, 5*time.Second, deferred, "the work it deferred did not run")
		ran := make(chan struct{})
		w.Exec(func(*Tx) { close(ran) })
		within(t, 5*time.Second, ran, "the next Exec did not run")

		faults := sink.all(FaultTransaction)
		if len(faults) != 1 || faults[0].Value != expectedPanic("test: exec panic") || faults[0].World != w {
			t.Fatalf("faults = %v, want the panic of the Exec", sink.all(0))
		}
		if !strings.Contains(faults[0].Type, "TestExecPanicIsLogged") {
			t.Fatalf("fault type = %q, want the function that panicked", faults[0].Type)
		}
		stack := string(faults[0].Stack)
		if !strings.Contains(stack, "TestExecPanicIsLogged") || !strings.Contains(stack, "queued by") || !strings.Contains(stack, "fault_boundary_test.go") {
			t.Fatalf("stack does not show the callback and the caller of Exec:\n%s", stack)
		}
		if lines := rec.errors(); len(lines) != 1 || lines[0].Message != "transaction panicked" {
			t.Fatalf("Error lines = %d, want 1 about the transaction", len(lines))
		}
	})
}

// openPanicType is an entity type whose Open panics once armed.
type openPanicType struct {
	taskTestEntityType
	armed *atomic.Bool
}

func (t openPanicType) Open(tx *Tx, handle *EntityHandle, data *EntityData) Entity {
	if t.armed.Load() {
		panic(faultcheck.Expected{Why: "test: open panic"})
	}
	return t.taskTestEntityType.Open(tx, handle, data)
}

func TestEntityOpenPanicFailsTheTask(t *testing.T) {
	isolated(t, func(t *testing.T) {
		var sink faultSink
		rec := newFaultRecorder()
		w := sink.configure(Config{Provider: NopProvider{}, RandomTickSpeed: -1}, rec).New()
		defer func() { _ = w.Close() }()

		armed := new(atomic.Bool)
		h := NewEntity(openPanicType{armed: armed}, taskTestEntityConfig{})
		within(t, 5*time.Second, w.Exec(func(tx *Tx) {
			tx.AddEntity(h)
			// The world opens the entity again for its next task.
			h.state.ent = nil
			h.state.tickerChecked = false
			armed.Store(true)
		}), "adding the entity did not end")

		called := false
		task := h.Do(func(*Tx, Entity) { called = true })
		within(t, 5*time.Second, task.Done(), "the entity task did not end")
		var perr *PanicError
		if err := task.Err(); !errors.As(err, &perr) || perr.Value != expectedPanic("test: open panic") || !errors.Is(err, ErrTaskPanicked) {
			t.Fatalf("task error = %v, want a *PanicError of the panic of Open", err)
		}
		if called {
			t.Fatal("the callback ran without an entity")
		}
		faults := sink.all(FaultTransaction)
		if len(faults) != 1 || faults[0].Entity != h || faults[0].Type != "dragonfly:test_entity" {
			t.Fatalf("faults = %v, want one blamed on the entity", sink.all(0))
		}

		// The world and the handle live: once Open works again, so do tasks.
		armed.Store(false)
		ran := make(chan struct{})
		w.Exec(func(*Tx) { close(ran) })
		within(t, 5*time.Second, ran, "the next Exec did not run")
		task = h.Do(func(*Tx, Entity) { called = true })
		within(t, 5*time.Second, task.Done(), "the next entity task did not end")
		if err := task.Err(); err != nil || !called {
			t.Fatalf("next entity task: err=%v called=%v", err, called)
		}
	})
}

func TestTaskPanicIsAFaultOfTheTransaction(t *testing.T) {
	var sink faultSink
	rec := newFaultRecorder()
	w := sink.configure(Config{Provider: NopProvider{}, Synchronous: true}, rec).New()
	t.Cleanup(func() { _ = w.Close() })

	task := w.Do(func(*Tx) { panic(faultcheck.Expected{Why: "test: task panic"}) })
	if err := task.Err(); !errors.Is(err, ErrTaskPanicked) {
		t.Fatalf("task error = %v, want ErrTaskPanicked", err)
	}
	faults := sink.all(FaultTransaction)
	if len(faults) != 1 || faults[0].Value != expectedPanic("test: task panic") || !strings.Contains(faults[0].Type, "TestTaskPanicIsAFaultOfTheTransaction") {
		t.Fatalf("faults = %v, want the panic of the task, blamed on the function that panicked", sink.all(0))
	}
	// A synchronous world recovers the panic of an Exec too.
	<-w.Exec(func(*Tx) { panic(faultcheck.Expected{Why: "test: exec panic"}) })
	if n := len(sink.all(FaultTransaction)); n != 2 {
		t.Fatalf("faults of the transaction = %d, want 2", n)
	}
}

// tickLog records the ticks something happened in.
type tickLog struct {
	mu    sync.Mutex
	ticks []int64
}

func (l *tickLog) add(tick int64) {
	l.mu.Lock()
	l.ticks = append(l.ticks, tick)
	l.mu.Unlock()
}

func (l *tickLog) all() []int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.ticks)
}

// probe returns an entity at pos calling tick in its tick.
func probe(pos mgl64.Vec3, tick func(tx *Tx, e *testEntity)) *EntityHandle {
	return EntitySpawnOpts{Position: pos}.New(testEntityType{}, regionProbeConfig{tick: tick})
}

// waitFor polls cond until it holds, ending the process after d as within
// does.
func waitFor(t *testing.T, d time.Duration, what string, cond func() bool) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		for !cond() {
			time.Sleep(5 * time.Millisecond)
		}
		close(done)
	}()
	within(t, d, done, what)
}

// loadAround loads the columns within radius of pos for a loader of its
// own, which keeps their entities ticking.
func loadAround(t *testing.T, w *World, pos mgl64.Vec3, radius int) {
	t.Helper()
	l := NewLoader(radius, w, NopViewer{})
	within(t, 10*time.Second, w.Exec(func(tx *Tx) { l.Move(tx, pos) }), "moving a loader")
	waitFor(t, 30*time.Second, "loading the columns", func() bool {
		<-w.Exec(func(tx *Tx) { l.Load(tx, 256) })
		l.mu.RLock()
		defer l.mu.RUnlock()
		return len(l.loadQueue) == 0
	})
}

// containsAll reports whether every tick of want is in got.
func containsAll(got, want []int64) bool {
	for _, tick := range want {
		if !slices.Contains(got, tick) {
			return false
		}
	}
	return true
}

func TestEntityTickPanicKeepsTheWorld(t *testing.T) {
	isolated(t, func(t *testing.T) {
		var sink faultSink
		rec := newFaultRecorder()
		w := sink.configure(Config{Provider: NopProvider{}, RandomTickSpeed: -1}, rec).New()
		defer func() { _ = w.Close() }()
		loadAround(t, w, mgl64.Vec3{}, 5)

		var panicked, counted tickLog
		bad := probe(mgl64.Vec3{1.5, 64, 1.5}, func(tx *Tx, _ *testEntity) {
			panicked.add(tx.CurrentTick())
			panic(faultcheck.Expected{Why: "test: entity tick"})
		})
		// The second entity is in the same column, after the first: the
		// column goes on after the panic.
		good := probe(mgl64.Vec3{2.5, 64, 2.5}, func(tx *Tx, _ *testEntity) { counted.add(tx.CurrentTick()) })
		within(t, 10*time.Second, w.Exec(func(tx *Tx) {
			tx.AddEntity(bad)
			tx.AddEntity(good)
		}), "adding the entities")

		waitFor(t, 30*time.Second, "the good entity ticking 40 times after the quarantine", func() bool {
			p := panicked.all()
			return len(p) >= FaultLimit && len(counted.all()) >= len(p)+40
		})
		if p := panicked.all(); len(p) != FaultLimit {
			t.Fatalf("the entity ticked %d times, want %d: it is not quarantined", len(p), FaultLimit)
		} else if !containsAll(counted.all(), p) {
			t.Fatalf("the other entity did not tick in the ticks of the panics %v", p)
		}
		faults := sink.all(FaultEntity)
		if len(faults) != FaultLimit {
			t.Fatalf("faults = %d, want %d", len(faults), FaultLimit)
		}
		for i, f := range faults {
			if f.Entity != bad || f.Count != i+1 || f.Quarantined != (i+1 == FaultLimit) || f.Type != "dragonfly:test_entity" || f.Pos != (cube.Pos{1, 64, 1}) {
				t.Fatalf("fault %d = %+v", i, f)
			}
		}
		if lines := rec.errors(); len(lines) != 1 || lines[0].Message != "entity panicked" || !strings.Contains(attr(lines[0], "stack"), "TestEntityTickPanicKeepsTheWorld") || attr(lines[0], "uuid") != bad.UUID().String() {
			t.Fatalf("Error lines = %d, want 1 with the stack and UUID, the 2 others suppressed", len(lines))
		}
		if q := w.Quarantined(); len(q) != 1 || q[0].Entity != bad {
			t.Fatalf("Quarantined = %v, want the entity", q)
		}
		within(t, 10*time.Second, w.Exec(func(tx *Tx) {
			if _, ok := bad.Entity(tx); !ok {
				t.Error("the quarantined entity left the world")
			}
			pos := ChunkPos{0, 0}
			saved := false
			for _, e := range w.columnTo(w.chunks[pos], pos).Entities {
				saved = saved || e.ID == bad.UniqueID()
			}
			if !saved {
				t.Error("the quarantined entity is not saved with its column")
			}
			tx.RemoveEntity(bad.mustEntity(tx))
		}), "checking the quarantined entity")
		if q := w.Quarantined(); len(q) != 0 {
			t.Fatalf("Quarantined after the entity left = %v", q)
		}
	})
}

// regionWorld returns an asynchronous world ticking its entities in
// regions, with loaders at the two positions far apart passed.
func regionWorld(t *testing.T, sink *faultSink, rec faultRecorder, a, b mgl64.Vec3) *World {
	t.Helper()
	w := sink.configure(Config{Provider: NopProvider{}, RandomTickSpeed: -1, Regions: true}, rec).New()
	loadAround(t, w, a, 4)
	loadAround(t, w, b, 4)
	return w
}

func TestRegionEntityPanicStaysInItsRegion(t *testing.T) {
	isolated(t, func(t *testing.T) {
		var sink faultSink
		rec := newFaultRecorder()
		a, b := mgl64.Vec3{8.5, 64, 8.5}, mgl64.Vec3{40*16 + 8.5, 64, 8.5}
		w := regionWorld(t, &sink, rec, a, b)
		defer func() { _ = w.Close() }()

		var panicked, sameRegion, otherRegion tickLog
		var regions sync.Map
		bad := probe(a, func(tx *Tx, _ *testEntity) {
			regions.Store("bad", tx.region)
			panicked.add(tx.CurrentTick())
			panic(faultcheck.Expected{Why: "test: region entity tick"})
		})
		next := probe(a.Add(mgl64.Vec3{1, 0, 1}), func(tx *Tx, _ *testEntity) { sameRegion.add(tx.CurrentTick()) })
		far := probe(b, func(tx *Tx, _ *testEntity) {
			regions.Store("far", tx.region)
			otherRegion.add(tx.CurrentTick())
		})
		within(t, 10*time.Second, w.Exec(func(tx *Tx) {
			tx.AddEntity(bad)
			tx.AddEntity(next)
			tx.AddEntity(far)
		}), "adding the entities")

		waitFor(t, 30*time.Second, "the other entities ticking 20 times after the quarantine", func() bool {
			p := panicked.all()
			return len(p) >= FaultLimit && len(otherRegion.all()) >= len(p)+20 && len(sameRegion.all()) >= len(p)+20
		})
		p := panicked.all()
		if len(p) != FaultLimit {
			t.Fatalf("the entity ticked %d times, want %d", len(p), FaultLimit)
		}
		if !containsAll(otherRegion.all(), p) || !containsAll(sameRegion.all(), p) {
			t.Fatalf("the other entities did not tick in the ticks of the panics %v", p)
		}
		badRegion, _ := regions.Load("bad")
		farRegion, _ := regions.Load("far")
		if badRegion.(*region) == nil || farRegion.(*region) == nil || badRegion == farRegion {
			t.Fatalf("the entities ticked in regions %v and %v, want two", badRegion, farRegion)
		}
		if f := sink.fatals(); len(f) != 0 {
			t.Fatalf("fatal faults = %v, want none", f)
		}
		if faults := sink.all(FaultEntity); len(faults) != FaultLimit || !faults[FaultLimit-1].Quarantined || faults[0].Entity != bad {
			t.Fatalf("faults = %v", sink.all(0))
		}
		if w.RegionPhases() == 0 {
			t.Fatal("no region phase ran")
		}
	})
}

func TestRegionPanicHoldingTheSharedLockIsFatal(t *testing.T) {
	isolated(t, func(t *testing.T) {
		var sink faultSink
		rec := newFaultRecorder()
		a, b := mgl64.Vec3{8.5, 64, 8.5}, mgl64.Vec3{40*16 + 8.5, 64, 8.5}
		w := regionWorld(t, &sink, rec, a, b)
		defer func() { _ = w.Close() }()

		var panicked, otherRegion tickLog
		bad := probe(a, func(tx *Tx, _ *testEntity) {
			panicked.add(tx.CurrentTick())
			tx.lockShared()
			panic(faultcheck.Expected{Why: "test: panic holding the shared lock"})
		})
		far := probe(b, func(tx *Tx, _ *testEntity) { otherRegion.add(tx.CurrentTick()) })
		within(t, 10*time.Second, w.Exec(func(tx *Tx) {
			tx.AddEntity(bad)
			tx.AddEntity(far)
		}), "adding the entities")

		waitFor(t, 30*time.Second, "the world failing", func() bool { return len(sink.fatals()) != 0 })
		// The failed world simulates no more: neither entity ticks again.
		ticked := len(otherRegion.all())
		time.Sleep(300 * time.Millisecond)
		if p := panicked.all(); len(p) != 1 || len(otherRegion.all()) != ticked {
			t.Fatalf("the failed world ticked its entities: %d panics, %d ticks of the other after %d", len(p), len(otherRegion.all()), ticked)
		}
		fatal := sink.fatals()
		if len(fatal) != 1 || fatal[0].Kind != FaultFatal || fatal[0].Entity != bad || fatal[0].Value != expectedPanic("test: panic holding the shared lock") {
			t.Fatalf("Config.Fatal calls = %v, want one, blamed on the entity", fatal)
		}
		if faults := sink.all(FaultEntity); len(faults) != 0 {
			t.Fatalf("entity faults = %v: a panic holding the shared lock is not the entity's alone", faults)
		}
		within(t, 10*time.Second, w.Exec(func(tx *Tx) {
			if !w.regions.mu.TryLock() {
				t.Error("the shared lock is still held")
				return
			}
			w.regions.mu.Unlock()
		}), "checking the shared lock")
		if n := len(rec.errors()); n != 1 {
			t.Fatalf("Error lines = %d, want one for the fatal panic", n)
		}
	})
}

// advance ticks the synchronous world w, failing the test if the tick
// panicked.
func advance(t *testing.T, w *World) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("the tick panicked: %v", r)
		}
	}()
	w.AdvanceTick()
}

func TestRegionWorkPanicIsBlamedOnItsEntity(t *testing.T) {
	var sink faultSink
	rec := newFaultRecorder()
	w := sink.configure(Config{Synchronous: true, Regions: true}, rec).New()
	defer func() { _ = w.Close() }()

	var ran []string
	a := probe(mgl64.Vec3{8.5, 4, 8.5}, func(tx *Tx, _ *testEntity) {
		// Left to the owner after the phase, which runs the work after it
		// anyway.
		tx.WholeWorld(func(*Tx) { panic(faultcheck.Expected{Why: "test: region work"}) })
		tx.WholeWorld(func(*Tx) { ran = append(ran, "after") })
	})
	b := probe(mgl64.Vec3{1024 + 8.5, 4, 8.5}, func(*Tx, *testEntity) {})
	<-w.Exec(func(tx *Tx) {
		tx.AddEntity(a)
		tx.AddEntity(b)
	})
	advance(t, w)
	if w.RegionPhases() != 1 {
		t.Fatalf("%d region phases, want 1", w.RegionPhases())
	}
	if len(ran) != 1 {
		t.Fatalf("the work after the panic ran %d times, want once", len(ran))
	}
	if faults := sink.all(0); len(faults) != 1 || faults[0].Kind != FaultEntity || faults[0].Entity != a || faults[0].Count != 1 {
		t.Fatalf("faults = %v, want one blamed on the entity", faults)
	}
}

// faulterEntity is a testEntity that handles the panics of its tick, as
// players do.
type faulterEntity struct {
	*testEntity
	panicked *[]*Tx
}

func (e faulterEntity) TickPanicked(tx *Tx, f Fault) {
	*e.panicked = append(*e.panicked, tx)
	if f.Entity != e.handle {
		panic("fault of another entity")
	}
}

type faulterType struct {
	testEntityType
	panicked *[]*Tx
}

func (t faulterType) Open(tx *Tx, handle *EntityHandle, data *EntityData) Entity {
	return faulterEntity{testEntity: t.testEntityType.Open(tx, handle, data).(*testEntity), panicked: t.panicked}
}

// A TickFaulter handles the panics of its tick on the owner, after the
// phase if it ticked in a region, and is never quarantined.
func TestTickFaulterHandlesItsPanicsOnTheOwner(t *testing.T) {
	var sink faultSink
	w := sink.configure(Config{Synchronous: true, Regions: true}, newFaultRecorder()).New()
	defer func() { _ = w.Close() }()

	var panicked []*Tx
	ticks := 0
	a := EntitySpawnOpts{Position: mgl64.Vec3{8.5, 4, 8.5}}.New(faulterType{panicked: &panicked}, regionProbeConfig{tick: func(tx *Tx, _ *testEntity) {
		ticks++
		if tx.region == nil {
			t.Error("the entity ticked outside a region")
		}
		panic(faultcheck.Expected{Why: "test: faulter tick"})
	}})
	b := probe(mgl64.Vec3{1024 + 8.5, 4, 8.5}, func(*Tx, *testEntity) {})
	<-w.Exec(func(tx *Tx) {
		tx.AddEntity(a)
		tx.AddEntity(b)
	})
	for range FaultLimit + 2 {
		advance(t, w)
	}
	if ticks != FaultLimit+2 || len(panicked) != ticks {
		t.Fatalf("ticked %d times, handled %d panics: want %d of each, never quarantined", ticks, len(panicked), FaultLimit+2)
	}
	for _, tx := range panicked {
		if tx.region != nil {
			t.Fatal("TickPanicked ran in a region")
		}
	}
	if q := w.Quarantined(); len(q) != 0 {
		t.Fatalf("Quarantined = %v", q)
	}
	if faults := sink.all(FaultEntity); len(faults) != FaultLimit+2 || faults[0].Count != 0 {
		t.Fatalf("faults = %d, want %d, not counted", len(faults), FaultLimit+2)
	}
}

func TestBlockEntityTickPanicQuarantinesIt(t *testing.T) {
	var sink faultSink
	rec := newFaultRecorder()
	other := cancelBatchTestBlock{name: "test:other", id: 3, tick: func(cube.Pos, *Tx) {}}
	w := sink.configure(Config{Synchronous: true, RandomTickSpeed: -1, RandSource: actorIdentityRandom{}, Blocks: testBlockRegistry(other)}, rec).New()
	defer func() { _ = w.Close() }()

	badPos, goodPos := cube.Pos{0, 4, 0}, cube.Pos{1, 4, 0}
	bad, good := &actorIdentityTicker{}, &actorIdentityTicker{}
	bad.callback = func(*Tx) { panic(faultcheck.Expected{Why: "test: block entity tick"}) }
	var col *Column
	<-w.Exec(func(tx *Tx) {
		col = tx.chunk(ChunkPos{})
		// The panicking one first: the pass goes on after it.
		col.updateBlockEntity(badPos, bad)
		col.updateBlockEntity(goodPos, good)
	})
	for range FaultLimit + 2 {
		advance(t, w)
	}
	if bad.ticks != FaultLimit || good.ticks != FaultLimit+2 {
		t.Fatalf("ticks: panicking %d, other %d, want %d and %d", bad.ticks, good.ticks, FaultLimit, FaultLimit+2)
	}
	faults := sink.all(FaultBlockEntity)
	if len(faults) != FaultLimit || faults[0].Pos != badPos || faults[0].Type != "dragonfly:test_ticker" || !faults[FaultLimit-1].Quarantined || faults[0].Quarantined {
		t.Fatalf("faults = %v", faults)
	}
	if q := w.Quarantined(); len(q) != 1 || q[0].Pos != badPos || q[0].Kind != FaultBlockEntity {
		t.Fatalf("Quarantined = %v, want the block entity", q)
	}
	if lines := rec.errors(); len(lines) != 1 {
		t.Fatalf("Error lines = %d, want 1", len(lines))
	}

	// Its data changing keeps it quarantined: it is the same block entity.
	<-w.Exec(func(*Tx) { col.updateBlockEntity(badPos, bad) })
	advance(t, w)
	if bad.ticks != FaultLimit {
		t.Fatalf("the quarantined block entity ticked after its data changed")
	}
	// Removed by a block set there, it is no longer quarantined, and the
	// block entity placed there next ticks. (The block set differs from air,
	// which the position holds: a block set to its own state changes nothing.)
	<-w.Exec(func(tx *Tx) { tx.SetBlock(badPos, other, nil) })
	if q := w.Quarantined(); len(q) != 0 {
		t.Fatalf("Quarantined after the block entity was removed = %v", q)
	}
	replacement := &actorIdentityTicker{}
	<-w.Exec(func(*Tx) { col.updateBlockEntity(badPos, replacement) })
	advance(t, w)
	if replacement.ticks != 1 {
		t.Fatalf("the block entity replacing the quarantined one ticked %d times, want 1", replacement.ticks)
	}
}

func TestScheduledTickPanicDropsTheTick(t *testing.T) {
	var sink faultSink
	rec := newFaultRecorder()
	registry := NewBlockRegistry()
	badPos, goodPos := cube.Pos{0, 65, 0}, cube.Pos{1, 65, 0}
	var badTicks, goodTicks int
	var bad, good cancelBatchTestBlock
	bad = cancelBatchTestBlock{name: "test:panicking", id: 11, tick: func(pos cube.Pos, tx *Tx) {
		badTicks++
		// It schedules itself again, then panics: a storm, once it no
		// longer brings the world down.
		tx.ScheduleBlockUpdate(pos, bad, time.Second/20)
		panic(faultcheck.Expected{Why: "test: scheduled tick"})
	}}
	good = cancelBatchTestBlock{name: "test:counting", id: 12, tick: func(pos cube.Pos, tx *Tx) {
		goodTicks++
		tx.ScheduleBlockUpdate(pos, good, time.Second/20)
	}}
	other := cancelBatchTestBlock{name: "test:other", id: 13, tick: func(cube.Pos, *Tx) {}}
	for _, b := range []cancelBatchTestBlock{bad, good, other} {
		registry.RegisterBlockState(BlockState{Name: b.name, Properties: map[string]any{}})
		registry.RegisterBlock(b)
	}
	w := sink.configure(Config{Synchronous: true, Blocks: registry}, rec).New()
	defer func() { _ = w.Close() }()
	<-w.Exec(func(tx *Tx) {
		// The panicking block first: the batch goes on after it.
		tx.SetBlock(badPos, bad, nil)
		tx.SetBlock(goodPos, good, nil)
		tx.ScheduleBlockUpdate(badPos, bad, time.Second/20)
		tx.ScheduleBlockUpdate(goodPos, good, time.Second/20)
	})
	for range FaultLimit + 2 {
		advance(t, w)
		if w.scheduledBlockTicks.processing {
			t.Fatal("the queue is still processing after the tick")
		}
	}
	if badTicks != FaultLimit || goodTicks != FaultLimit+2 {
		t.Fatalf("ticks: panicking %d, other %d, want %d and %d", badTicks, goodTicks, FaultLimit, FaultLimit+2)
	}
	faults := sink.all(FaultScheduledTick)
	if len(faults) != FaultLimit || faults[0].Pos != badPos || faults[0].Type != "test:panicking" || !faults[FaultLimit-1].Quarantined {
		t.Fatalf("faults = %v", faults)
	}
	if q := w.Quarantined(); len(q) != 1 || q[0].Pos != badPos || q[0].Kind != FaultScheduledTick {
		t.Fatalf("Quarantined = %v, want the block", q)
	}
	if lines := rec.errors(); len(lines) != 1 {
		t.Fatalf("Error lines = %d, want 1", len(lines))
	}
	scheduled := func() bool {
		_, ok := w.scheduledBlockTicks.scheduledTicks[scheduledTickIndex{pos: badPos, typeName: "test:panicking"}]
		return ok
	}
	<-w.Exec(func(tx *Tx) {
		if tx.ScheduleBlockUpdate(badPos, bad, time.Second/20); scheduled() {
			t.Error("a tick of the quarantined block was scheduled")
		}
		// The block set again, as a block changing its state does, is still
		// the block that panicked.
		tx.SetBlock(badPos, bad, nil)
	})
	if q := w.Quarantined(); len(q) != 1 {
		t.Fatalf("Quarantined after the block was set again = %v", q)
	}
	// Replaced by another block, it is forgotten: placed again, it ticks.
	<-w.Exec(func(tx *Tx) { tx.SetBlock(badPos, other, nil) })
	if q := w.Quarantined(); len(q) != 0 {
		t.Fatalf("Quarantined after the block was replaced = %v", q)
	}
	<-w.Exec(func(tx *Tx) {
		tx.SetBlock(badPos, bad, nil)
		if tx.ScheduleBlockUpdate(badPos, bad, time.Second/20); !scheduled() {
			t.Error("the block placed again got no tick")
		}
	})
	advance(t, w)
	if badTicks != FaultLimit+1 {
		t.Fatalf("the block placed again ticked %d times in all, want %d", badTicks, FaultLimit+1)
	}
}

// randomTestBlock is a block whose random ticks call tick.
type randomTestBlock struct{ cancelBatchTestBlock }

func (b randomTestBlock) RandomTick(pos cube.Pos, tx *Tx, _ *rand.Rand) { b.tick(pos, tx) }

// neighbourTestBlock is a block whose neighbour updates call tick.
type neighbourTestBlock struct{ cancelBatchTestBlock }

func (b neighbourTestBlock) NeighbourUpdateTick(pos, _ cube.Pos, tx *Tx) { b.tick(pos, tx) }

// rainTestBlock is a full block that rain falling onto calls tick.
type rainTestBlock struct{ cancelBatchTestBlock }

func (rainTestBlock) Model() BlockModel                                           { return solidTestModel{} }
func (b rainTestBlock) HandlePrecipitation(pos cube.Pos, tx *Tx, _ Precipitation) { b.tick(pos, tx) }

// solidTestModel is the model of a full block.
type solidTestModel struct{}

func (solidTestModel) BBox(cube.Pos, BlockSource) []cube.BBox {
	return []cube.BBox{cube.Box(0, 0, 0, 1, 1, 1)}
}
func (solidTestModel) FaceSolid(cube.Pos, cube.Face, BlockSource) bool { return true }

// zeroRandom is a source of zeros: random ticks then pick the first block
// of each sub chunk, and every precipitation attempt is made.
type zeroRandom struct{}

func (zeroRandom) Uint64() uint64 { return 0 }

// testBlockRegistry returns a registry of the blocks passed.
func testBlockRegistry(blocks ...Block) BlockRegistry {
	registry := NewBlockRegistry()
	for _, b := range blocks {
		name, _ := b.EncodeBlock()
		registry.RegisterBlockState(BlockState{Name: name, Properties: map[string]any{}})
		registry.RegisterBlock(b)
	}
	return registry
}

// checkBlockQuarantine checks that the panics of the block at pos, named
// name, of kind are those of FaultLimit panics, the last of which
// quarantined it, with one Error line.
func checkBlockQuarantine(t *testing.T, w *World, sink *faultSink, rec faultRecorder, kind FaultKind, pos cube.Pos, name string) {
	t.Helper()
	faults := sink.all(kind)
	if len(faults) != FaultLimit {
		t.Fatalf("faults = %v, want %d of %v", sink.all(0), FaultLimit, kind)
	}
	for i, f := range faults {
		if f.Pos != pos || f.Type != name || f.Count != i+1 || f.Quarantined != (i+1 == FaultLimit) || len(f.Stack) == 0 {
			t.Fatalf("fault %d = %+v", i, f)
		}
	}
	if q := w.Quarantined(); len(q) != 1 || q[0].Pos != pos || q[0].Kind != kind || q[0].Type != name {
		t.Fatalf("Quarantined = %v, want the block", q)
	}
	if lines := rec.errors(); len(lines) != 1 || lines[0].Message != kind.String()+" panicked" {
		t.Fatalf("Error lines = %d, want 1", len(lines))
	}
}

func TestRandomTickPanic(t *testing.T) {
	var sink faultSink
	rec := newFaultRecorder()
	badPos, goodPos := cube.Pos{0, 0, 0}, cube.Pos{0, 16, 0}
	var badTicks, goodTicks int
	bad := randomTestBlock{cancelBatchTestBlock{name: "test:random_panicking", id: 21, tick: func(pos cube.Pos, tx *Tx) {
		badTicks++
		// Setting itself first, as a block changing its state does, it is
		// still the block that panicked.
		tx.SetBlock(pos, tx.Block(pos), nil)
		panic(faultcheck.Expected{Why: "test: random tick"})
	}}}
	good := randomTestBlock{cancelBatchTestBlock{name: "test:random_counting", id: 22, tick: func(cube.Pos, *Tx) { goodTicks++ }}}
	other := cancelBatchTestBlock{name: "test:other", id: 23, tick: func(cube.Pos, *Tx) {}}
	// A source of zeros has the first block of each sub chunk tick: the
	// panicking one, then the other one in the sub chunk above.
	conf := Config{Synchronous: true, Blocks: testBlockRegistry(bad, good, other), RandomTickSpeed: 1, RandSource: zeroRandom{}}
	w := sink.configure(conf, rec).New()
	defer func() { _ = w.Close() }()
	<-w.Exec(func(tx *Tx) {
		tx.SetBlock(badPos, bad, nil)
		tx.SetBlock(goodPos, good, nil)
	})
	for range FaultLimit + 2 {
		advance(t, w)
	}
	if badTicks != FaultLimit || goodTicks != FaultLimit+2 {
		t.Fatalf("random ticks: panicking %d, other %d, want %d and %d", badTicks, goodTicks, FaultLimit, FaultLimit+2)
	}
	checkBlockQuarantine(t, w, &sink, rec, FaultRandomTick, badPos, "test:random_panicking")

	// Replaced by another block, it is forgotten: placed again, it ticks.
	<-w.Exec(func(tx *Tx) { tx.SetBlock(badPos, other, nil) })
	if q := w.Quarantined(); len(q) != 0 {
		t.Fatalf("Quarantined after the block was replaced = %v", q)
	}
	<-w.Exec(func(tx *Tx) { tx.SetBlock(badPos, bad, nil) })
	advance(t, w)
	if badTicks != FaultLimit+1 {
		t.Fatalf("the block placed again ticked %d times in all, want %d", badTicks, FaultLimit+1)
	}
}

func TestNeighbourUpdatePanic(t *testing.T) {
	var sink faultSink
	rec := newFaultRecorder()
	// The updates of a block set at trigger reach badPos, east of it,
	// before goodPos, west of it.
	trigger, badPos, goodPos := cube.Pos{1, 64, 0}, cube.Pos{2, 64, 0}, cube.Pos{0, 64, 0}
	var badTicks, goodTicks int
	bad := neighbourTestBlock{cancelBatchTestBlock{name: "test:neighbour_panicking", id: 31, tick: func(cube.Pos, *Tx) {
		badTicks++
		panic(faultcheck.Expected{Why: "test: neighbour update"})
	}}}
	good := neighbourTestBlock{cancelBatchTestBlock{name: "test:neighbour_counting", id: 32, tick: func(cube.Pos, *Tx) { goodTicks++ }}}
	other := cancelBatchTestBlock{name: "test:other", id: 33, tick: func(cube.Pos, *Tx) {}}
	w := sink.configure(Config{Synchronous: true, Blocks: testBlockRegistry(bad, good, other), RandomTickSpeed: -1}, rec).New()
	defer func() { _ = w.Close() }()
	quiet := &SetOpts{DisableBlockUpdates: true}
	<-w.Exec(func(tx *Tx) {
		tx.SetBlock(badPos, bad, quiet)
		tx.SetBlock(goodPos, good, quiet)
	})
	update := func(i int) {
		<-w.Exec(func(tx *Tx) {
			if i%2 == 0 {
				tx.SetBlock(trigger, other, nil)
			} else {
				tx.SetBlock(trigger, nil, nil)
			}
		})
		advance(t, w)
	}
	for i := range FaultLimit + 2 {
		update(i)
		if w.neighbourUpdates.count != 0 {
			t.Fatalf("%d neighbour updates left after the tick", w.neighbourUpdates.count)
		}
	}
	if badTicks != FaultLimit || goodTicks != FaultLimit+2 {
		t.Fatalf("neighbour updates: panicking %d, other %d, want %d and %d", badTicks, goodTicks, FaultLimit, FaultLimit+2)
	}
	checkBlockQuarantine(t, w, &sink, rec, FaultNeighbourUpdate, badPos, "test:neighbour_panicking")

	// Set again as itself, it stays quarantined.
	<-w.Exec(func(tx *Tx) { tx.SetBlock(badPos, bad, quiet) })
	update(0)
	if badTicks != FaultLimit {
		t.Fatalf("the quarantined block was updated after it was set again")
	}
	// Unloading its chunk forgets it.
	<-w.Exec(func(tx *Tx) { w.closeChunk(tx, ChunkPos{}, w.chunks[ChunkPos{}]) })
	if q := w.Quarantined(); len(q) != 0 || len(w.blockFaults.m) != 0 {
		t.Fatalf("Quarantined after the chunk unloaded = %v", q)
	}
}

// rainTestBiome is a biome it rains in.
type rainTestBiome struct{ unknownBiome }

func (rainTestBiome) HasPrecipitation() bool { return true }
func (rainTestBiome) String() string         { return "test:rain" }

var registerRainTestBiome = sync.OnceValue(func() Biome {
	b := rainTestBiome{unknownBiome{id: 59990}}
	RegisterBiome(b)
	return b
})

func TestPrecipitationPanicIsARandomTickFault(t *testing.T) {
	var sink faultSink
	rec := newFaultRecorder()
	pos := cube.Pos{0, 64, 0}
	var ticks int
	bad := rainTestBlock{cancelBatchTestBlock{name: "test:rain_panicking", id: 41, tick: func(cube.Pos, *Tx) {
		ticks++
		panic(faultcheck.Expected{Why: "test: precipitation"})
	}}}
	w := sink.configure(Config{Synchronous: true, Blocks: testBlockRegistry(bad), RandomTickSpeed: 1, RandSource: zeroRandom{}}, rec).New()
	defer func() { _ = w.Close() }()
	biome := registerRainTestBiome()
	w.set.Lock()
	w.set.Raining, w.set.WeatherCycle = true, false
	w.set.Unlock()
	<-w.Exec(func(tx *Tx) {
		tx.SetBlock(pos, bad, nil)
		tx.SetBiome(pos, biome)
	})
	for range FaultLimit + 2 {
		// The next attempt of the chunk is at the column of the block: its
		// state times 3 plus 0x3c6ef35f is 0.
		<-w.Exec(func(tx *Tx) {
			state := uint32(0x3c6ef35f)
			tx.chunk(ChunkPos{}).snowRandomValue = -state * 0xaaaaaaab
		})
		advance(t, w)
	}
	if ticks != FaultLimit {
		t.Fatalf("the rain fell onto the block %d times, want %d", ticks, FaultLimit)
	}
	checkBlockQuarantine(t, w, &sink, rec, FaultRandomTick, pos, "test:rain_panicking")
}

// storeCountingProvider is a provider counting the columns stored.
type storeCountingProvider struct {
	NopProvider
	stores atomic.Int32
}

func (p *storeCountingProvider) StoreColumn(pos ChunkPos, dim Dimension, col *chunk.Column) error {
	p.stores.Add(1)
	return p.NopProvider.StoreColumn(pos, dim, col)
}

func TestTickGluePanicIsFatal(t *testing.T) {
	isolated(t, func(t *testing.T) {
		var sink faultSink
		rec := newFaultRecorder()
		prov := &storeCountingProvider{}
		other := cancelBatchTestBlock{name: "test:other", id: 51, tick: func(cube.Pos, *Tx) {}}
		conf := Config{Provider: prov, Blocks: testBlockRegistry(other), RandomTickSpeed: -1}
		w := sink.configure(conf, rec).New()
		closed := false
		defer func() {
			if !closed {
				_ = w.Close()
			}
		}()

		var ticks atomic.Int32
		within(t, 10*time.Second, w.Exec(func(tx *Tx) {
			// A changed column, which closing the world saves.
			tx.SetBlock(cube.Pos{0, 64, 0}, other, nil)
			w.testTickHook = func(*Tx) {
				if ticks.Add(1) == 5 {
					panic(faultcheck.Expected{Why: "test: tick glue"})
				}
			}
		}), "arming the hook")
		waitFor(t, 10*time.Second, "the world failing", func() bool { return len(sink.fatals()) != 0 })
		fatal := sink.fatals()
		if len(fatal) != 1 || fatal[0].Kind != FaultFatal || fatal[0].Value != expectedPanic("test: tick glue") || fatal[0].World != w || !strings.Contains(fatal[0].Type, "TestTickGluePanicIsFatal") {
			t.Fatalf("Config.Fatal calls = %v, want one for the panic of the tick", fatal)
		}
		if !strings.Contains(string(fatal[0].Stack), "fault_boundary_test.go") {
			t.Fatalf("the fault has not the stack of the panic:\n%s", fatal[0].Stack)
		}

		// The failed world simulates nothing, but its transactions run.
		var before, after int64
		within(t, 10*time.Second, w.Exec(func(tx *Tx) { before = tx.CurrentTick() }), "an Exec of the failed world")
		time.Sleep(300 * time.Millisecond)
		within(t, 10*time.Second, w.Exec(func(tx *Tx) { after = tx.CurrentTick() }), "an Exec of the failed world")
		if before != after || ticks.Load() != 5 {
			t.Fatalf("the failed world simulated: tick %d, then %d; the hook ran %d times, want 5", before, after, ticks.Load())
		}
		if n := len(rec.errors()); n != 1 {
			t.Fatalf("Error lines = %d, want 1", n)
		}

		// Closing it still saves it.
		stored := prov.stores.Load()
		closed = true
		if err := w.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
		if prov.stores.Load() <= stored {
			t.Fatal("closing the failed world saved no column")
		}
		if n := len(sink.fatals()); n != 1 {
			t.Fatalf("Config.Fatal called %d times, want once", n)
		}
	})
}

func TestSubsystemPanicFailsTheWorldAfterTicksInARow(t *testing.T) {
	var sink faultSink
	rec := newFaultRecorder()
	var (
		collisions, spawns int
		armed              bool
	)
	entities := EntityRegistryConfig{
		EntityCollisions: func(*Tx) {
			if !armed {
				return
			}
			collisions++
			// It panics in 10 ticks, then not in one, then in 20.
			if collisions != 11 {
				panic(faultcheck.Expected{Why: "test: collisions"})
			}
		},
		NaturalSpawnTick: func(*Tx) {
			if armed {
				spawns++
			}
		},
	}.New(nil)
	// New ticks a synchronous world once already.
	w := sink.configure(Config{Synchronous: true, Entities: entities, RandomTickSpeed: -1}, rec).New()
	defer func() { _ = w.Close() }()
	armed = true

	for range 10 + 1 + stepFaultLimit - 1 {
		advance(t, w)
	}
	if spawns != collisions || spawns != 10+1+stepFaultLimit-1 {
		t.Fatalf("the rest of the tick did not run: %d spawn ticks for %d of collisions", spawns, collisions)
	}
	faults := sink.all(FaultSubsystem)
	if len(faults) != 10+stepFaultLimit-1 || faults[0].Type != "entity collisions" || faults[9].Count != 10 || faults[10].Count != 1 {
		t.Fatalf("faults = %v", faults)
	}
	if f := sink.fatals(); len(f) != 0 {
		t.Fatalf("the world failed before %d ticks in a row: %v", stepFaultLimit, f)
	}
	if n := len(rec.errors()); n != 1 {
		t.Fatalf("Error lines = %d, want 1, the others suppressed", n)
	}
	advance(t, w)
	fatal := sink.fatals()
	if len(fatal) != 1 || fatal[0].Kind != FaultFatal || fatal[0].Type != "entity collisions" || fatal[0].Count != stepFaultLimit {
		t.Fatalf("Config.Fatal calls = %v, want one after %d ticks in a row", fatal, stepFaultLimit)
	}
	// The failed world simulates nothing.
	ticked := spawns
	advance(t, w)
	if spawns != ticked {
		t.Fatal("the failed world ticked its subsystems")
	}
}

// redstonePanicSource is a redstone source whose power panics.
type redstonePanicSource struct{ cancelBatchTestBlock }

func (redstonePanicSource) RedstonePower(cube.Pos, *Tx, cube.Face) int {
	panic(faultcheck.Expected{Why: "test: redstone"})
}

func TestRedstonePanicDropsItsInput(t *testing.T) {
	var sink faultSink
	rec := newFaultRecorder()
	source := redstonePanicSource{cancelBatchTestBlock{name: "test:redstone_panicking", id: 61, tick: func(cube.Pos, *Tx) {}}}
	consumer := redstoneCancellationConsumer{}
	registry := testBlockRegistry(source)
	registry.RegisterBlockState(BlockState{Name: "test:redstone_consumer", Properties: map[string]any{"powered": false}})
	registry.RegisterBlockState(BlockState{Name: "test:redstone_consumer", Properties: map[string]any{"powered": true}})
	registry.RegisterBlock(consumer)
	registry.RegisterBlock(redstoneCancellationConsumer{Powered: true})
	w := sink.configure(Config{Synchronous: true, Blocks: registry, RandomTickSpeed: -1}, rec).New()
	defer func() { _ = w.Close() }()
	<-w.Exec(func(tx *Tx) {
		tx.SetBlock(cube.Pos{0, 64, 0}, consumer, nil)
		tx.SetBlock(cube.Pos{1, 64, 0}, source, nil)
	})
	advance(t, w)
	advance(t, w)
	faults := sink.all(FaultSubsystem)
	if len(faults) != 1 || faults[0].Type != "redstone" || faults[0].Value != expectedPanic("test: redstone") {
		t.Fatalf("faults = %v, want one of redstone", sink.all(0))
	}
	if n := len(w.redstone.dirty); n != 0 {
		t.Fatalf("%d positions left to evaluate after the panic", n)
	}
}

// failingTestViewer is a viewer whose ViewBlockUpdates panics, counting
// the times it was told it failed.
type failingTestViewer struct {
	NopViewer
	failed *atomic.Int32
}

func (failingTestViewer) ViewBlockUpdates(*BlockUpdates) {
	panic(faultcheck.Expected{Why: "test: view block updates"})
}
func (v failingTestViewer) ViewFailed() { v.failed.Add(1) }

// countingTestViewer counts the changes of blocks it is shown.
type countingTestViewer struct {
	NopViewer
	updates *atomic.Int32
}

func (v countingTestViewer) ViewBlockUpdates(u *BlockUpdates) { v.updates.Add(int32(len(u.Updates))) }

func TestViewerPanicFailsTheViewer(t *testing.T) {
	isolated(t, func(t *testing.T) {
		var sink faultSink
		rec := newFaultRecorder()
		other := cancelBatchTestBlock{name: "test:other", id: 81, tick: func(cube.Pos, *Tx) {}}
		w := sink.configure(Config{Provider: NopProvider{}, Blocks: testBlockRegistry(other), RandomTickSpeed: -1}, rec).New()
		defer func() { _ = w.Close() }()
		failed, updates := new(atomic.Int32), new(atomic.Int32)
		loaders := []*Loader{NewLoader(1, w, failingTestViewer{failed: failed}), NewLoader(1, w, countingTestViewer{updates: updates})}
		within(t, 10*time.Second, w.Exec(func(tx *Tx) {
			for _, l := range loaders {
				l.Move(tx, mgl64.Vec3{8, 64, 8})
			}
		}), "moving the loaders")
		waitFor(t, 30*time.Second, "loading the columns", func() bool {
			<-w.Exec(func(tx *Tx) {
				for _, l := range loaders {
					l.Load(tx, 16)
				}
			})
			for _, l := range loaders {
				l.mu.RLock()
				n := len(l.loadQueue)
				l.mu.RUnlock()
				if n != 0 {
					return false
				}
			}
			return true
		})

		// An entity task sets a block, which the work after it shows to
		// the viewers of the column: the panicking viewer is told, the
		// other shown the block, and the task ends.
		h := probe(mgl64.Vec3{8.5, 64, 8.5}, func(*Tx, *testEntity) {})
		within(t, 10*time.Second, w.Exec(func(tx *Tx) { tx.AddEntity(h) }), "adding the entity")
		task := h.Do(func(tx *Tx, _ Entity) { tx.SetBlock(cube.Pos{1, 64, 1}, other, nil) })
		within(t, 10*time.Second, task.Done(), "the entity task did not end")
		// So does a task of the world.
		task = w.Do(func(tx *Tx) { tx.SetBlock(cube.Pos{2, 64, 1}, other, nil) })
		within(t, 10*time.Second, task.Done(), "the task of the world did not end")

		if n := failed.Load(); n != 2 {
			t.Fatalf("the panicking viewer was told it failed %d times, want 2", n)
		}
		if n := updates.Load(); n != 2 {
			t.Fatalf("the other viewer was shown %d changes of blocks, want 2", n)
		}
		faults := sink.all(FaultViewer)
		if len(faults) != 2 || faults[0].Type != "world.failingTestViewer" || faults[0].Value != expectedPanic("test: view block updates") || !strings.Contains(string(faults[0].Stack), "fault_boundary_test.go") {
			t.Fatalf("faults = %v, want two of the viewer", sink.all(0))
		}
	})
}

// expectedPanic returns the value of a panic of a test that panics on
// purpose, for the test to compare a fault with.
func expectedPanic(why string) any { return faultcheck.Expected{Why: why} }
