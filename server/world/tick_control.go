package world

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"sync"
	"time"
)

// Frozen reports whether automatic world simulation is paused.
func (w *World) Frozen() bool { return w != nil && w.tickFrozen.Load() }

// Freeze pauses entity and block simulation until Unfreeze or Step.
func (w *World) Freeze() {
	if w != nil {
		w.tickFrozen.Store(true)
	}
}

// Unfreeze resumes normal world simulation and clears any pending step budget.
func (w *World) Unfreeze() {
	if w != nil {
		w.tickFrozen.Store(false)
		w.stepTicks.Store(0)
	}
}

// StepTicks advances a frozen world for the requested number of ticks.
func (w *World) StepTicks(n int) error {
	if w == nil {
		return errors.New("world unavailable")
	}
	if n < 1 {
		return errors.New("step tick count must be positive")
	}
	w.tickFrozen.Store(true)
	w.stepTicks.Store(int64(n))
	return nil
}

// StopStepping cancels a pending frozen-world step.
func (w *World) StopStepping() {
	if w != nil {
		w.stepTicks.Store(0)
	}
}

// SprintTicks runs the requested number of ticks at the fastest ticker cadence.
func (w *World) SprintTicks(n int) error {
	if w == nil {
		return errors.New("world unavailable")
	}
	if n < 1 {
		return errors.New("sprint tick count must be positive")
	}
	w.tickFrozen.Store(false)
	w.stepTicks.Store(0)
	w.sprintTicks.Store(int64(n))
	return nil
}

// StopSprinting ends an active sprint.
func (w *World) StopSprinting() {
	if w != nil {
		w.sprintTicks.Store(0)
	}
}

// TickRate returns the configured non-synchronous tick rate, in ticks per second.
func (w *World) TickRate() float64 {
	if w == nil {
		return 20
	}
	rate := math.Float64frombits(w.tickRate.Load())
	if rate == 0 {
		return 20
	}
	return rate
}

// SetTickRate changes the rate used by non-synchronous world's ticker.
func (w *World) SetTickRate(rate float64) error {
	if w == nil {
		return errors.New("world unavailable")
	}
	if math.IsNaN(rate) || math.IsInf(rate, 0) || rate < 1 || rate > 10000 {
		return fmt.Errorf("tick rate must be between 1 and 10000")
	}
	w.tickRate.Store(math.Float64bits(rate))
	return nil
}

// Sprinting reports whether the world runs a sprint of ticks.
func (w *World) Sprinting() bool { return w != nil && w.sprintTicks.Load() > 0 }

// tickTimeSamples is how many of the last ticks TickTimes returns, as
// MinecraftServer keeps the times of its last 100 ticks.
const tickTimeSamples = 100

// tickTimes holds how long the last ticks of a world took.
type tickTimes struct {
	mu      sync.Mutex
	samples [tickTimeSamples]time.Duration
	sum     time.Duration
	count   int
}

// record adds the time a tick took.
func (t *tickTimes) record(d time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	i := t.count % tickTimeSamples
	t.sum += d - t.samples[i]
	t.samples[i] = d
	t.count++
}

// TickTimes returns the average time the last 100 ticks of the world took
// and those times, as MinecraftServer.getAverageTickTimeNanos and
// getTickTimesNanos do. Fewer are returned until the world ticked 100
// times.
func (w *World) TickTimes() (average time.Duration, samples []time.Duration) {
	if w == nil {
		return 0, nil
	}
	t := &w.tickTimes
	t.mu.Lock()
	defer t.mu.Unlock()
	n := min(t.count, tickTimeSamples)
	if n == 0 {
		return 0, nil
	}
	return t.sum / time.Duration(n), slices.Clone(t.samples[:n])
}
