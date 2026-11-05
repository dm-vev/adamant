package world

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"
)

// ErrOwnerWait is returned by Call, CallEntity, CallRef, Task.Wait and the
// waiting functions built on them, and by World.Close, when called from a
// world owner or a region worker: waiting there deadlocks on the same owner,
// and may deadlock on another, which may be waiting for this one. World.Save
// logs it and returns without saving. A task a call scheduled is cancelled if
// it has not started.
var ErrOwnerWait = errors.New("world: waiting for a world owner from a world owner")

// PanicOnOwnerWait makes a wait for a world owner from a world owner panic,
// with the stack of the wait, instead of failing with ErrOwnerWait. The panic
// runs on a goroutine of its own, so that no recover, such as that of a task,
// stops it: it ends the program. Tests set it in TestMain, before any world
// runs, so that the suite catches every such wait; it is read at each wait.
var PanicOnOwnerWait bool

var (
	// owners holds the goroutines that run transactions of a world: the
	// goroutine of each World that is not synchronous, and the region
	// workers while they tick a region. It maps the ID of the goroutine to
	// the World.
	owners sync.Map
	// ownerCount is the number of goroutines in owners, so that a wait
	// looks its goroutine up only when there are owners at all.
	ownerCount atomic.Int64
	// ownerWaitLogged is the time, in Unix nanoseconds, a wait from an
	// owner was last logged.
	ownerWaitLogged atomic.Int64
)

// ownerWaitLogInterval is the least time between two logs of waits from
// owners.
const ownerWaitLogInterval = 10 * time.Second

// goroutineID returns the ID of the calling goroutine, which Go has no API
// for: it is read from the first line of its stack, "goroutine 47 [running]:",
// whose format has not changed since Go 1.0. TestGoroutineID checks it on the
// toolchain the tests run with.
func goroutineID() uint64 {
	var buf [64]byte
	b := buf[:runtime.Stack(buf[:], false)]
	const prefix = "goroutine "
	if len(b) < len(prefix) || string(b[:len(prefix)]) != prefix {
		return 0
	}
	var id uint64
	for _, c := range b[len(prefix):] {
		if c < '0' || c > '9' {
			break
		}
		id = id*10 + uint64(c-'0')
	}
	return id
}

// enterOwner records the calling goroutine as one running transactions of w.
// It returns the ID of the goroutine and whether it was not an owner yet, in
// which case leaveOwner must be called with the ID once it no longer is: a
// region worker that ticks a region on the owner's goroutine stays the
// owner.
func enterOwner(w *World) (id uint64, entered bool) {
	id = goroutineID()
	if id == 0 {
		return 0, false
	}
	if _, loaded := owners.LoadOrStore(id, w); loaded {
		return id, false
	}
	ownerCount.Add(1)
	return id, true
}

// leaveOwner removes the goroutine of the ID passed from the owners.
func leaveOwner(id uint64) {
	if _, ok := owners.LoadAndDelete(id); ok {
		ownerCount.Add(-1)
	}
}

// ownerOf returns the World whose transactions the calling goroutine runs,
// nil if it runs none.
func ownerOf() *World {
	if ownerCount.Load() == 0 {
		return nil
	}
	if w, ok := owners.Load(goroutineID()); ok {
		return w.(*World)
	}
	return nil
}

// checkOwnerWait returns ErrOwnerWait if the calling goroutine is a world
// owner or a region worker, about to wait for what: it logs the stack of the
// wait, at most once every ownerWaitLogInterval. With PanicOnOwnerWait, it
// panics instead.
func checkOwnerWait(what string) error {
	w := ownerOf()
	if w == nil {
		return nil
	}
	stack := debug.Stack()
	if PanicOnOwnerWait {
		msg := fmt.Sprintf("%v: %s on the owner of world %q\n\n%s", ErrOwnerWait, what, w.Name(), stack)
		// A panic of this goroutine may be recovered by a task: one on its
		// own ends the program.
		fmt.Fprintln(os.Stderr, msg)
		go func() { panic(msg) }()
		select {}
	}
	now := time.Now().UnixNano()
	if last := ownerWaitLogged.Load(); now-last >= int64(ownerWaitLogInterval) && ownerWaitLogged.CompareAndSwap(last, now) {
		w.log.Error("Waiting for a world owner from a world owner: failing instead of deadlocking.", "wait", what, "world", w.Name(), "stack", string(stack))
	}
	return ErrOwnerWait
}
