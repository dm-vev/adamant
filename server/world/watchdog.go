package world

import (
	"runtime"
	"time"
)

const (
	// watchdogInterval is how often the watchdog of a world looks at its
	// tick.
	watchdogInterval = 5 * time.Second
	// watchdogTimeout is how long a tick may run before the watchdog logs
	// the stacks of all goroutines.
	watchdogTimeout = 60 * time.Second
)

// watchdog watches the ticks of a world, as the watchdogs of Paper and
// Spigot do the main thread. A tick still running after watchdogTimeout,
// which an infinite loop or a deadlock causes and no recover ends, has the
// stacks of all goroutines logged once. It kills nothing: the tick may still
// end.
type watchdog struct {
	w   *World
	now func() time.Time
	// reported is the start of the last tick reported, in nanoseconds since
	// the Unix epoch.
	reported int64
}

// watchTicks runs the watchdog of w until it closes.
func (w *World) watchTicks() {
	defer w.running.Done()
	d := watchdog{w: w, now: time.Now}
	t := time.NewTicker(watchdogInterval)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			d.check()
		case <-w.closing:
			return
		}
	}
}

// check logs the stacks of all goroutines if the tick of the world began
// watchdogTimeout ago or more and was not reported yet, and reports whether
// it did.
func (d *watchdog) check() bool {
	start := d.w.tickStarted.Load()
	if start == 0 || start == d.reported {
		return false
	}
	running := d.now().Sub(time.Unix(0, start))
	if running < watchdogTimeout {
		return false
	}
	d.reported = start
	d.w.log.Error("A tick has run for a long time: it may never end. The stacks of all goroutines follow.", "running", running.Round(time.Second), "stack", string(goroutineStacks()))
	return true
}

// goroutineStacks returns the stacks of all goroutines, at most 64 MiB of
// them.
func goroutineStacks() []byte {
	buf := make([]byte, 1<<20)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) || len(buf) >= 64<<20 {
			return buf[:n]
		}
		buf = make([]byte, 2*len(buf))
	}
}
