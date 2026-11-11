package world

import (
	"sync"
	"time"
)

// txQueue is the queue of transactions of the world, and the count of scheduled
// work that close drains.
type txQueue struct {
	queue         chan transaction
	queueClosing  chan struct{}
	queueMu       sync.Mutex
	queueOverflow []transaction
	queueWake     chan struct{}
	queueing      sync.WaitGroup
	// scheduleMu serialises task scheduling against the close transitions
	// below. scheduling counts in-flight scheduled work that close must drain.
	scheduleMu sync.Mutex
	scheduling sync.WaitGroup
}

// transaction is a type that may be added to the transaction queue of a World.
// Its Run method is called when the transaction is taken out of the queue.
type transaction interface {
	Run(w *World)
}

// execFunc is a function that performs a synchronised transaction on a World.
type execFunc func(tx *Tx)

// ExecFunc is kept for Adamant callers that still use the legacy blocking API.
type ExecFunc func(tx *Tx)

// Exec performs a legacy blocking transaction. New off-owner code should use
// Do or Call so scheduling cannot block the caller when the queue is full.
//
// A panic of f is logged as a fault of the transaction, with the caller of
// Exec, and ends the transaction: the channel still closes.
func (w *World) Exec(f ExecFunc) <-chan struct{} {
	return w.execNormal(normalTransaction{f: f, caller: callerPC(0)})
}

// exec runs f on the World, bypassing the closed check that Do/DoAfter/Call
// apply — reserved for the World's own machinery (ticking, saving, chunk
// unload, the close transaction), which must queue work after close begins.
// The returned channel closes when done; waiting on it from the owner deadlocks.
func (w *World) exec(f execFunc) <-chan struct{} {
	return w.execNormal(normalTransaction{f: f, caller: callerPC(0)})
}

// execTick runs the tick f on the World as exec does. A tick has panic
// boundaries of its own.
func (w *World) execTick(f execFunc) <-chan struct{} {
	return w.execNormal(normalTransaction{f: f, tick: true})
}

// execNormal queues ntx, giving it a channel that closes once it ran.
func (w *World) execNormal(ntx normalTransaction) <-chan struct{} {
	c := make(chan struct{})
	if w == nil {
		// Nil worlds are treated as no-ops; return a closed channel so callers don't block.
		close(c)
		return c
	}
	ntx.c = c
	if w.conf.Synchronous {
		ntx.Run(w)
		return c
	}
	select {
	case w.queue <- ntx:
	case <-w.queueClosing:
		close(c)
	}
	return c
}

func (w *World) weakExec(valid func() bool, cond *sync.Cond, f execFunc, allowClosed bool) <-chan bool {
	c := make(chan bool, 1)
	if w.conf.Synchronous {
		run := valid == nil || valid()
		if run {
			// As in weakTransaction.Run, f must not run under cond.L: it may
			// relock it, e.g. through RemoveEntity.
			cond.L.Unlock()
			tx := newTx(w)
			f(tx)
			tx.close()
			tx.runDeferred()
			cond.L.Lock()
		}
		c <- run
		return c
	}
	w.scheduleMu.Lock()
	if w.closed.Load() && !w.closeAcceptingEntityTasks.Load() && !allowClosed {
		w.scheduleMu.Unlock()
		c <- false
		return c
	}
	wtx := weakTransaction{c: c, f: f, valid: valid, cond: cond}
	w.enqueueTransaction(wtx)
	w.scheduleMu.Unlock()
	return c
}

// enqueueTransaction queues tx without blocking the owner. Once the bounded
// channel fills, the owner drains the overflow itself instead of creating one
// blocked goroutine per request.
func (w *World) enqueueTransaction(tx transaction) {
	w.queueMu.Lock()
	defer w.queueMu.Unlock()

	if len(w.queueOverflow) == 0 {
		select {
		case w.queue <- tx:
			return
		default:
		}
	}
	w.scheduling.Add(1)
	w.queueOverflow = append(w.queueOverflow, tx)
	select {
	case w.queueWake <- struct{}{}:
	default:
	}
}

func (w *World) nextOverflowTransaction() (transaction, bool) {
	w.queueMu.Lock()
	defer w.queueMu.Unlock()
	if len(w.queueOverflow) == 0 {
		return nil, false
	}
	tx := w.queueOverflow[0]
	w.queueOverflow[0] = nil
	w.queueOverflow = w.queueOverflow[1:]
	return tx, true
}

// handleTransactions continuously reads transactions from the queue and runs
// them.
func (w *World) handleTransactions() {
	if id, entered := enterOwner(w); entered {
		defer leaveOwner(id)
	}
	for {
		select {
		case tx := <-w.queue:
			w.runTransaction(tx)
			continue
		default:
		}
		if tx, ok := w.nextOverflowTransaction(); ok {
			w.runTransaction(tx)
			w.scheduling.Done()
			continue
		}
		select {
		case tx := <-w.queue:
			w.runTransaction(tx)
		case <-w.queueWake:
		case <-w.queueClosing:
			w.queueing.Done()
			return
		}
	}
}

// runTransaction runs tx and then spreads the light of the chunks that
// became complete during it, as chunks finishing generation do. Light
// readers bring it up to date anyway, but spreading it here, between ticks,
// keeps a tick from first spreading the light of every chunk generated
// since the last one, as Java lights chunks off its main thread.
func (w *World) runTransaction(tx transaction) {
	start := time.Now()
	tx.Run(w)
	if len(w.pendingSpreads) > 0 {
		w.flushSpreads()
	}
	w.stats.OwnerBusy.Add(uint64(time.Since(start)))
}
