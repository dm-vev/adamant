// Package faultcheck lets tests see the panics that production code
// recovers.
//
// A world recovers the panic of a transaction, a tick or a viewer so that one
// bug does not take the server down. A test that panics inside one of them
// stops there and still passes, with only a log line to show for it. Every
// site that recovers a panic and goes on calls Report; a test binary calls
// Start in its TestMain (see testfixture.RunCheckingPanics) and fails once
// Take returns what was recovered.
//
// The package does not import "testing", as production code imports it.
package faultcheck

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
)

// Recovered is a panic that a recovery site swallowed.
type Recovered struct {
	// Site names the recovery site: "world transaction", "viewer",
	// "session packet" and the like, with the type it was blamed on.
	Site string
	// Value is the recovered panic value.
	Value any
	// Stack is the top of the stack of the goroutine that recovered it, which
	// holds the frames of the panic as a recovery site is called by a
	// deferred function.
	Stack string
}

// Expected is the panic value of a test that panics on purpose to see a
// recovery site recover it. Report ignores it. Fields are free to say what
// the panic stands for.
type Expected struct {
	// Why is what the test panics for.
	Why any
}

// Error implements error, so that an Expected panic reads as what it is
// for, Why, in logs and reports.
func (e Expected) Error() string { return fmt.Sprint(e.Why) }

var (
	recording atomic.Bool
	mu        sync.Mutex
	recovered []Recovered
)

// maxRecorded bounds what is kept, so a loop of panics does not fill the
// memory of a test.
const maxRecorded = 1024

// stackFrames is how many frames of the stack Report keeps: those of the
// recovery site and of the panic below it, which names the test or the code
// that panicked.
const stackFrames = 24

// Start turns recording on, for the rest of the process.
func Start() { recording.Store(true) }

// Report records that site recovered the panic value. It does nothing unless
// Start was called, or the value is an Expected.
func Report(site string, value any) {
	if !recording.Load() {
		return
	}
	if _, ok := value.(Expected); ok {
		return
	}
	stack := shortStack()
	mu.Lock()
	if len(recovered) < maxRecorded {
		recovered = append(recovered, Recovered{Site: site, Value: value, Stack: stack})
	}
	mu.Unlock()
}

// shortStack returns the first stackFrames frames of the calling goroutine
// above Report, one "function file:line" per line.
func shortStack() string {
	var pcs [stackFrames]uintptr
	n := runtime.Callers(3, pcs[:])
	frames := runtime.CallersFrames(pcs[:n])
	var b strings.Builder
	for {
		fr, more := frames.Next()
		fmt.Fprintf(&b, "%s\n\t%s:%d\n", fr.Function, fr.File, fr.Line)
		if !more {
			return b.String()
		}
	}
}

// Take returns what was recorded and clears it.
func Take() []Recovered {
	mu.Lock()
	defer mu.Unlock()
	r := recovered
	recovered = nil
	return r
}
