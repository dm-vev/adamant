package schedule

import (
	"context"
	"sync"

	"github.com/df-mc/dragonfly/sdk/plugin"
	"github.com/df-mc/dragonfly/server/world"
)

// AsyncTask represents an off-thread background computation (e.g. database or
// HTTP I/O) whose result can be marshalled safely back onto a world transaction
// via ThenWorld or handled via Then. If bound to a *plugin.Context, its context
// is automatically cancelled when the plugin disables.
type AsyncTask[T any] struct {
	cancel context.CancelFunc

	mu      sync.Mutex
	done    bool
	val     T
	err     error
	hooks   []func(T, error)
	stopped bool
}

// Async starts work in a background goroutine with a cancellable context. When
// ctx is non-nil, the work context is cancelled automatically on plugin disable
// and pending callbacks are suppressed.
func Async[T any](ctx *plugin.Context, work func(context.Context) (T, error)) *AsyncTask[T] {
	runCtx, cancel := context.WithCancel(context.Background())
	t := &AsyncTask[T]{cancel: cancel}
	if ctx != nil {
		ctx.OnDisable(t.Cancel)
	}
	if work == nil {
		t.done = true
		cancel()
		return t
	}
	go func() {
		defer cancel()
		val, err := work(runCtx)
		t.mu.Lock()
		t.val = val
		t.err = err
		t.done = true
		stopped := t.stopped
		hooks := t.hooks
		t.hooks = nil
		t.mu.Unlock()
		if stopped {
			return
		}
		for _, fn := range hooks {
			fn(val, err)
		}
	}()
	return t
}

// Cancel cancels the background context and suppresses any pending completion
// callbacks. Safe to call multiple times.
func (t *AsyncTask[T]) Cancel() {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.stopped = true
	t.hooks = nil
	t.mu.Unlock()
	if t.cancel != nil {
		t.cancel()
	}
}

// Then registers fn to run when the background work finishes (on the worker
// goroutine if still running, or immediately if already finished). Suppressed
// if the task or owning plugin was cancelled.
func (t *AsyncTask[T]) Then(fn func(val T, err error)) *AsyncTask[T] {
	if t == nil || fn == nil {
		return t
	}
	t.mu.Lock()
	if t.stopped {
		t.mu.Unlock()
		return t
	}
	if t.done {
		val, err := t.val, t.err
		t.mu.Unlock()
		fn(val, err)
		return t
	}
	t.hooks = append(t.hooks, fn)
	t.mu.Unlock()
	return t
}

// ThenWorld delivers the background result safely inside a transaction on w.
// Suppressed if the task or owning plugin was cancelled before delivery.
func (t *AsyncTask[T]) ThenWorld(w *world.World, fn func(tx *world.Tx, val T, err error)) *AsyncTask[T] {
	if t == nil || w == nil || fn == nil {
		return t
	}
	return t.Then(func(val T, err error) {
		w.Exec(func(tx *world.Tx) {
			t.mu.Lock()
			stopped := t.stopped
			t.mu.Unlock()
			if !stopped {
				fn(tx, val, err)
			}
		})
	})
}
