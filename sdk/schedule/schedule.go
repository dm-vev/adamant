// Package schedule runs world tick systems and delayed/repeating tasks safely
// inside world transactions, with automatic cleanup when a plugin disables.
package schedule

import (
	"sync/atomic"
	"time"

	"github.com/df-mc/dragonfly/sdk/plugin"
	"github.com/df-mc/dragonfly/server/world"
)

// Phase selects when a world tick system executes within an active tick.
type Phase = world.TickPhase

const (
	PreEntities  = world.PhasePreEntities
	PostEntities = world.PhasePostEntities
	PostTick     = world.PhasePostTick
)

const tickDuration = 50 * time.Millisecond

// Task represents a delayed or repeating task scheduled on a world.
type Task struct {
	cancelled atomic.Bool
	remove    func()
}

// Cancel stops the task from firing again. Safe to call multiple times or from
// inside the task callback itself.
func (t *Task) Cancel() {
	if t == nil || !t.cancelled.CompareAndSwap(false, true) {
		return
	}
	if t.remove != nil {
		t.remove()
	}
}

// Cancelled reports whether the task has been cancelled or completed.
func (t *Task) Cancelled() bool {
	if t == nil {
		return true
	}
	return t.cancelled.Load()
}

// System registers fn to execute during phase on each active tick of w. When
// ctx is non-nil, the system is automatically removed on plugin disable.
func System(ctx *plugin.Context, w *world.World, phase Phase, fn func(tx *world.Tx, tick int64)) func() {
	if w == nil || fn == nil {
		return func() {}
	}
	remove := w.AddSystem(phase, fn)
	if ctx != nil {
		ctx.OnDisable(remove)
	}
	return remove
}

// After schedules fn to run once on w after delayTicks simulation ticks (>= 1).
func After(ctx *plugin.Context, w *world.World, delayTicks int64, fn func(tx *world.Tx)) *Task {
	return newTask(ctx, w, max(delayTicks, 1), 0, fn)
}

// Every schedules fn to run repeatedly on w every intervalTicks ticks (>= 1).
func Every(ctx *plugin.Context, w *world.World, intervalTicks int64, fn func(tx *world.Tx)) *Task {
	interval := max(intervalTicks, 1)
	return newTask(ctx, w, interval, interval, fn)
}

// AfterDuration schedules fn to run once on w after approximately d (20 TPS).
func AfterDuration(ctx *plugin.Context, w *world.World, d time.Duration, fn func(tx *world.Tx)) *Task {
	return After(ctx, w, durationToTicks(d), fn)
}

// EveryDuration schedules fn to run repeatedly on w every d (20 TPS).
func EveryDuration(ctx *plugin.Context, w *world.World, d time.Duration, fn func(tx *world.Tx)) *Task {
	return Every(ctx, w, durationToTicks(d), fn)
}

func newTask(ctx *plugin.Context, w *world.World, delayTicks, intervalTicks int64, fn func(tx *world.Tx)) *Task {
	t := &Task{}
	if w == nil || fn == nil {
		t.cancelled.Store(true)
		return t
	}
	remaining := delayTicks
	t.remove = w.AddSystem(world.PhasePostTick, func(tx *world.Tx, _ int64) {
		if t.cancelled.Load() {
			return
		}
		remaining--
		if remaining > 0 {
			return
		}
		if intervalTicks <= 0 {
			t.Cancel()
			fn(tx)
			return
		}
		remaining = intervalTicks
		fn(tx)
	})
	if ctx != nil {
		ctx.OnDisable(t.Cancel)
	}
	return t
}

func durationToTicks(d time.Duration) int64 {
	if d <= tickDuration {
		return 1
	}
	return int64((d + tickDuration/2) / tickDuration)
}
