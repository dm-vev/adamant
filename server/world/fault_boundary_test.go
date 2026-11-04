package world

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
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
			panic("test: exec panic")
		})
		within(t, 5*time.Second, done, "the panicking Exec did not end")
		within(t, 5*time.Second, deferred, "the work it deferred did not run")
		ran := make(chan struct{})
		w.Exec(func(*Tx) { close(ran) })
		within(t, 5*time.Second, ran, "the next Exec did not run")

		faults := sink.all(FaultTransaction)
		if len(faults) != 1 || faults[0].Value != "test: exec panic" || faults[0].World != w {
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
		panic("test: open panic")
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
		if err := task.Err(); !errors.As(err, &perr) || perr.Value != "test: open panic" || !errors.Is(err, ErrTaskPanicked) {
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

	task := w.Do(func(*Tx) { panic("test: task panic") })
	if err := task.Err(); !errors.Is(err, ErrTaskPanicked) {
		t.Fatalf("task error = %v, want ErrTaskPanicked", err)
	}
	faults := sink.all(FaultTransaction)
	if len(faults) != 1 || faults[0].Value != "test: task panic" || !strings.Contains(faults[0].Type, "TestTaskPanicIsAFaultOfTheTransaction") {
		t.Fatalf("faults = %v, want the panic of the task, blamed on the function that panicked", sink.all(0))
	}
	// A synchronous world recovers the panic of an Exec too.
	<-w.Exec(func(*Tx) { panic("test: exec panic") })
	if n := len(sink.all(FaultTransaction)); n != 2 {
		t.Fatalf("faults of the transaction = %d, want 2", n)
	}
}
