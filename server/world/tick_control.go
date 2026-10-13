package world

import (
	"errors"
	"fmt"
	"math"
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
