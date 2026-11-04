package world

import "time"

// TickState is the state of the ticking of a world: whether a tick is
// queued or runs and since when, and when the last one ended. The probes
// of the health of a server read it. It is of the times the tick loop
// stores as it ticks, tickStarted, which the watchdog of stuck ticks reads
// too, and tickEnded, and never waits for the owner.
type TickState struct {
	// Ticking is whether the world ticks on its own, 20 times a second: it
	// is not Synchronous and not closed.
	Ticking bool
	// Running is whether the tick loop asked the owner of the world for a
	// tick that has not ended.
	Running bool
	// Started is when the tick running was queued, zero between ticks.
	// Measured from it, as the watchdog measures, a tick that waits for an
	// owner busy with another transaction counts as running too.
	Started time.Time
	// Ended is when the last tick the tick loop waited for ended, zero
	// before the first.
	Ended time.Time
	// Since is when the world was made, when it began ticking.
	Since time.Time
}

// TickState returns the state of the ticking of the world. It reads
// atomics alone, and may be called at any time from any goroutine.
func (w *World) TickState() TickState {
	if w == nil {
		return TickState{}
	}
	started, ended := w.tickStarted.Load(), w.tickEnded.Load()
	s := TickState{
		Ticking: !w.conf.Synchronous && !w.closed.Load(),
		Running: started != 0,
		Since:   w.made,
	}
	if started != 0 {
		s.Started = time.Unix(0, started)
	}
	if ended != 0 {
		s.Ended = time.Unix(0, ended)
	}
	return s
}
