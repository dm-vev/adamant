package cmd

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/dm-vev/adamant/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

type stubWorlds struct {
	worlds []*world.World
	online []*world.EntityHandle
}

func (s stubWorlds) Worlds() []*world.World                          { return s.worlds }
func (s stubWorlds) Online() []*world.EntityHandle                   { return s.online }
func (s stubWorlds) PlayerByName(string) (*world.EntityHandle, bool) { return nil, false }

type deferSource struct {
	mu      sync.Mutex
	outputs []*Output
	defers  atomic.Int32
	dones   atomic.Int32
	doneCh  chan struct{}
}

func newDeferSource() *deferSource {
	return &deferSource{doneCh: make(chan struct{}, 4)}
}

func (s *deferSource) Position() mgl64.Vec3 { return mgl64.Vec3{} }
func (s *deferSource) SendCommandOutput(o *Output) {
	s.mu.Lock()
	s.outputs = append(s.outputs, o)
	s.mu.Unlock()
}
func (s *deferSource) Defer() func() {
	s.defers.Add(1)
	return func() {
		s.dones.Add(1)
		s.doneCh <- struct{}{}
	}
}
func (s *deferSource) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.outputs)
}

type wrappedSource struct{ inner Source }

func (w wrappedSource) Position() mgl64.Vec3        { return w.inner.Position() }
func (w wrappedSource) SendCommandOutput(o *Output) { w.inner.SendCommandOutput(o) }
func (w wrappedSource) Unwrap() Source              { return w.inner }

func TestSetWorlds(t *testing.T) {
	prev, had := installedWorlds()
	t.Cleanup(func() {
		if had {
			SetWorlds(prev)
		} else {
			SetWorlds(nil)
		}
	})

	SetWorlds(nil)
	if w, ok := installedWorlds(); ok || w != nil {
		t.Fatalf("installedWorlds after SetWorlds(nil) = (%v, %v), want (nil, false)", w, ok)
	}

	s := stubWorlds{}
	SetWorlds(s)
	if w, ok := installedWorlds(); !ok || w == nil {
		t.Fatalf("installedWorlds after SetWorlds(s) = (%v, %v), want (s, true)", w, ok)
	}

	SetWorlds(nil)
	if w, ok := installedWorlds(); ok || w != nil {
		t.Fatalf("installedWorlds after clear = (%v, %v), want (nil, false)", w, ok)
	}
}

func TestLaterDeferrerAndAbort(t *testing.T) {
	src := newDeferSource()
	send := Later(wrappedSource{inner: src})
	if src.defers.Load() != 1 {
		t.Fatalf("defers = %d, want 1", src.defers.Load())
	}
	out := &Output{}
	out.Print("hello")
	send(nil, out)
	// Second call must be a no-op.
	send(nil, out)
	if src.dones.Load() != 1 || src.count() != 1 {
		t.Fatalf("dones=%d outputs=%d, want 1, 1", src.dones.Load(), src.count())
	}

	// Nil output signals abort: done runs, no output delivered.
	src2 := newDeferSource()
	send2 := Later(src2)
	send2(nil, nil)
	if src2.dones.Load() != 1 || src2.count() != 0 {
		t.Fatalf("abort: dones=%d outputs=%d, want 1, 0", src2.dones.Load(), src2.count())
	}
}

type asyncRunnable struct {
	onRun func(src Source, o *Output, tx *world.Tx)
}

func (r asyncRunnable) Run(src Source, o *Output, tx *world.Tx) {
	r.onRun(src, o, tx)
}

func TestHeldOutputAndObserver(t *testing.T) {
	var obsMu sync.Mutex
	var observed []Execution
	prevObs := observer.Load()
	SetObserver(func(e Execution) {
		obsMu.Lock()
		observed = append(observed, e)
		obsMu.Unlock()
	})
	t.Cleanup(func() {
		observer.Store(prevObs)
	})

	releaseCh := make(chan struct{})
	finalSrc := newDeferSource()

	c := New("held", "", nil, asyncRunnable{
		onRun: func(src Source, o *Output, _ *world.Tx) {
			o.acquire(src)
			o.acquire(src) // nested hold
			go func() {
				<-releaseCh
				o.Print("part 1")
				o.release(nil, finalSrc)
				o.Print("part 2")
				o.release(nil, finalSrc)
			}()
		},
	})

	src := newDeferSource()
	c.Execute("", src, nil)

	// Output must still be held after Execute returns.
	if src.count() != 0 || src.dones.Load() != 0 {
		t.Fatalf("before release: outputs=%d dones=%d, want 0, 0", src.count(), src.dones.Load())
	}
	obsMu.Lock()
	if len(observed) != 0 {
		obsMu.Unlock()
		t.Fatalf("observer fired before release: %d", len(observed))
	}
	obsMu.Unlock()

	close(releaseCh)
	<-src.doneCh

	if src.count() != 1 || src.dones.Load() != 1 {
		t.Fatalf("after release: outputs=%d dones=%d, want 1, 1", src.count(), src.dones.Load())
	}
	src.mu.Lock()
	msgs := src.outputs[0].Messages()
	src.mu.Unlock()
	if len(msgs) != 2 || msgs[0].String() != "part 1" || msgs[1].String() != "part 2" {
		t.Fatalf("messages = %v, want [part 1, part 2]", msgs)
	}

	obsMu.Lock()
	defer obsMu.Unlock()
	if len(observed) != 1 {
		t.Fatalf("observed count = %d, want 1", len(observed))
	}
	if observed[0].Source != finalSrc {
		t.Fatalf("observed source was not updated to finalSrc")
	}
}

func TestHeldOutputAbort(t *testing.T) {
	abortCh := make(chan struct{})
	c := New("aborted", "", nil, asyncRunnable{
		onRun: func(src Source, o *Output, _ *world.Tx) {
			o.acquire(src)
			go func() {
				<-abortCh
				o.abort()
			}()
		},
	})

	src := newDeferSource()
	c.Execute("", src, nil)
	close(abortCh)
	<-src.doneCh

	if src.count() != 0 || src.dones.Load() != 1 {
		t.Fatalf("after abort: outputs=%d dones=%d, want 0, 1", src.count(), src.dones.Load())
	}
}
