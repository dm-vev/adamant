package world

import (
	"fmt"
	"runtime"
	"sync"

	"github.com/df-mc/dragonfly/server/block/cube"
)

// Tx is the owner transaction handle passed to world callbacks. It is the
// only way to perform world operations and is valid only during its callback.
type Tx struct {
	w        *World
	closed   bool
	deferred []scheduledTransaction
	// rebound are the entities tx bound that were bound to a transaction
	// tx runs in (entityState.bind): close binds them back to it.
	rebound []rebinding
	// region is the region the transaction ticks the entities of in a
	// region phase, or nil for a transaction of the world owner.
	region    *region
	lastLocal lastLocal
}

// Context is a cancellable event scope passed to Handler events. It embeds the
// owner transaction, so world operations are available directly on it.
type Context struct {
	*Tx
	cancel bool
}

// newTx returns a fresh transaction on World w.
func newTx(w *World) *Tx {
	return &Tx{w: w}
}

// Event returns a fresh Context for dispatching one Handler event on this
// transaction, so cancelling one event cannot affect another.
func (tx *Tx) Event() *Context {
	return &Context{Tx: tx}
}

// Cancelled returns whether the Context has been cancelled by an event handler.
func (ctx *Context) Cancelled() bool { return ctx.cancel }

// Cancel cancels the Context. It is used by event handlers to signal that the
// default behaviour of the event should not run.
func (ctx *Context) Cancel() { ctx.cancel = true }

// Defer schedules f to run on the owner after the current callback completes
// and before the parent Task completes. Deferred callbacks run FIFO in
// registration order, unlike Go defer's LIFO order.
func (tx *Tx) Defer(f func(tx *Tx)) *Task {
	return tx.DeferErr(func(tx *Tx) error {
		f(tx)
		return nil
	})
}

// DeferErr schedules f to run on the owner after the current callback
// completes, recording any returned error on the Task.
func (tx *Tx) DeferErr(f func(tx *Tx) error) *Task {
	return tx.deferTask(f)
}

// Range returns the lower and upper bounds of the World that the Tx is
// operating on.
func (tx *Tx) Range() cube.Range {
	return tx.World().ra
}

func (tx *Tx) deferTask(f func(tx *Tx) error) *Task {
	if tx.closed {
		panic("world.Tx: use of transaction after transaction finishes is not permitted")
	}
	task := newTask()
	tx.deferred = append(tx.deferred, scheduledTransaction{task: task, f: f})
	return task
}

// World returns the Tx's World. It panics once the callback has
// completed. Treat the result as the off-owner handle: blocking calls like
// Save and Close deadlock from inside the callback, so do world operations
// through the Tx instead.
func (tx *Tx) World() *World {
	if tx.closed {
		panic("world.Tx: use of transaction after transaction finishes is not permitted")
	}
	return tx.w
}

// CurrentTick returns the current tick of the transaction's world.
func (tx *Tx) CurrentTick() int64 {
	w := tx.World()
	w.set.Lock()
	defer w.set.Unlock()
	return w.currentTickLocked()
}

// close finishes the Tx, causing any following call on the Tx to panic. The
// entities it bound that were bound to a transaction it ran in are bound
// back to it, the last bound first.
func (tx *Tx) close() {
	if !tx.closed {
		tx.w.flushLight()
	}
	tx.closed = true
	for i := len(tx.rebound) - 1; i >= 0; i-- {
		r := tx.rebound[i]
		if r.s.bound == tx && !r.prev.closed {
			r.s.bound = r.prev
			r.s.binder.BindTransaction(r.prev)
		}
	}
	tx.rebound = nil
}

// rebinding is an entity a transaction bound, and the transaction it was
// bound to before, which the first runs in.
type rebinding struct {
	s    *entityState
	prev *Tx
}

// nests reports whether inner may run inside tx, which it then binds the
// entities it uses back to as it closes: on a synchronous World, which runs
// a transaction started from a transaction at once, inner is of the same
// World and tx did not end. Regions tick in transactions of their own, which
// are none of these.
func (tx *Tx) nests(inner *Tx) bool {
	return !tx.closed && tx.w == inner.w && tx.w.conf.Synchronous && tx.region == nil && inner.region == nil
}

func (tx *Tx) runDeferred() {
	for len(tx.deferred) > 0 {
		deferred := tx.deferred
		tx.deferred = nil
		for _, st := range deferred {
			st.Run(tx.w)
		}
	}
	// The transaction ended: the viewers are shown the blocks it changed.
	tx.w.flushBlockUpdates()
}

// normalTransaction is added to the transaction queue for transactions created
// using World.exec().
type normalTransaction struct {
	c chan struct{}
	f func(tx *Tx)
	// tick is set for the transaction of a tick, which has panic
	// boundaries of its own.
	tick bool
	// caller is the program counter of the caller of Exec or exec.
	caller uintptr
}

// Run creates a *Tx, calls ntx.f, closes the transaction and finally closes
// ntx.c. A panic of ntx.f, unless ntx is a tick, is reported as a fault of
// the transaction, which ends there: its deferred work still runs.
func (ntx normalTransaction) Run(w *World) {
	tx := newTx(w)
	defer close(ntx.c)
	defer tx.close()
	if ntx.tick {
		ntx.f(tx)
	} else {
		ntx.call(w, tx)
	}
	tx.close()
	tx.runDeferred()
}

// call calls ntx.f, recovering a panic of it.
func (ntx normalTransaction) call(w *World, tx *Tx) {
	defer func() {
		if r := recover(); r != nil {
			f := newFault(FaultTransaction, r)
			f.Type = panicSite()
			if ntx.caller != 0 {
				fr, _ := runtime.CallersFrames([]uintptr{ntx.caller}).Next()
				f.Stack = fmt.Appendf(f.Stack, "\nqueued by %s\n\t%s:%d\n", fr.Function, fr.File, fr.Line)
			}
			w.reportFault(f, quarantineKey{})
		}
	}()
	ntx.f(tx)
}

// callerPC returns the program counter of the caller of the function
// calling callerPC, skip frames further up.
func callerPC(skip int) uintptr {
	var pc [1]uintptr
	if runtime.Callers(skip+3, pc[:]) == 0 {
		return 0
	}
	return pc[0]
}

// weakTransaction is a transaction that may be cancelled by its validity
// predicate before the transaction is run.
type weakTransaction struct {
	c     chan bool
	f     func(tx *Tx)
	valid func() bool
	cond  *sync.Cond
}

// Run runs the transaction, first checking if it is still valid and creating a
// *Tx if so. Afterwards, a bool indicating if the transaction was run is added
// to wtx.c. Finally, wtx.cond.Broadcast() is called. A panic of wtx.f is
// reported as a fault of the transaction: the transaction still counts as
// run, so that it is not tried again.
func (wtx weakTransaction) Run(w *World) {
	valid := wtx.valid == nil || wtx.valid()
	if valid {
		tx := newTx(w)
		func() {
			defer func() {
				tx.close()
				if r := recover(); r != nil {
					f := newFault(FaultTransaction, r)
					f.Type = panicSite()
					w.reportFault(f, quarantineKey{})
				}
			}()
			wtx.f(tx)
			tx.close()
			tx.runDeferred()
		}()
	}
	// We have to acquire a lock on wtx.cond.L here to make sure cond.Wait()
	// has been called before we call cond.Broadcast(). If not, we might
	// broadcast before cond.Wait() and cause a permanent suspension.
	wtx.cond.L.Lock()
	defer wtx.cond.L.Unlock()

	wtx.c <- valid
	wtx.cond.Broadcast()
}

// SaveWorld saves the world of the transaction like World.Save, from within
// the transaction, where Save would wait for the goroutine running it. As
// Java's save-all without flush, it does not wait for the chunks to reach
// the disk.
func (tx *Tx) SaveWorld() {
	if tx.region != nil {
		tx.deferToOwner(func(tx *Tx) { tx.SaveWorld() })
		return
	}
	_ = tx.World().capture(tx, false, nil).write(false)
}
