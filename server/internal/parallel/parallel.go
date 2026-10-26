// Package parallel runs loops over independent items on every core for the
// owner of a world, returning only once all items are done.
package parallel

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
)

// Panic is raised by For on its caller when f panicked on a worker. It keeps
// the original value and the stack of the worker.
type Panic struct {
	Value any
	Stack []byte
}

// Error implements error.
func (p *Panic) Error() string {
	return fmt.Sprintf("parallel worker panicked: %v\n%s", p.Value, p.Stack)
}

// Unwrap returns the original value if it was an error.
func (p *Panic) Unwrap() error {
	err, _ := p.Value.(error)
	return err
}

// Workers returns how many goroutines For runs a large loop on.
func Workers() int {
	return runtime.GOMAXPROCS(0)
}

// For calls f(i) for every i in [0, n) on up to Workers goroutines, the
// caller's included, handing out batches of grain items. It returns once
// every call finished. Loops of a single batch run on the caller alone. If f
// panics, the remaining batches are skipped and For panics on its caller
// with a *Panic once every worker stopped, so no worker outlives it.
//
// f must not wait for the caller's world: the caller does not run its world
// until For returns.
func For(n, grain int, f func(i int)) {
	ForWorkers(n, grain, func(_, i int) { f(i) })
}

// ForWorkers is For, passing f the index of the worker running it as well:
// a number below Workers that no other worker of the loop has at the same
// time, for indexing per-worker state.
func ForWorkers(n, grain int, f func(worker, i int)) {
	if n <= 0 {
		return
	}
	grain = max(grain, 1)
	workers := min(Workers(), (n+grain-1)/grain)
	if workers <= 1 {
		for i := range n {
			f(0, i)
		}
		return
	}
	var (
		next     atomic.Int64
		failed   atomic.Pointer[Panic]
		finished sync.WaitGroup
	)
	work := func(worker int) {
		defer finished.Done()
		defer func() {
			if r := recover(); r != nil {
				failed.CompareAndSwap(nil, &Panic{Value: r, Stack: debug.Stack()})
			}
		}()
		for failed.Load() == nil {
			hi := int(next.Add(int64(grain)))
			lo := hi - grain
			if lo >= n {
				return
			}
			for i := lo; i < min(hi, n); i++ {
				f(worker, i)
			}
		}
	}
	finished.Add(workers)
	for worker := 1; worker < workers; worker++ {
		go work(worker)
	}
	work(0)
	finished.Wait()
	if p := failed.Load(); p != nil {
		panic(p)
	}
}
