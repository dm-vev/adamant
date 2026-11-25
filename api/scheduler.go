package api

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/df-mc/dragonfly/server/world"
)

const tickDuration = 50 * time.Millisecond

// ScheduledTask represents a delayed or repeating tick task scheduled on a world.
type ScheduledTask struct {
	cancelled atomic.Bool
	remove    func()
	onCancel  func()
}

// Cancel stops the task from firing again. Safe to call multiple times and
// from inside the task callback itself.
func (t *ScheduledTask) Cancel() {
	if t == nil || !t.cancelled.CompareAndSwap(false, true) {
		return
	}
	if t.remove != nil {
		t.remove()
	}
	if t.onCancel != nil {
		t.onCancel()
	}
}

// Cancelled reports whether the task has been cancelled or completed.
func (t *ScheduledTask) Cancelled() bool {
	if t == nil {
		return true
	}
	return t.cancelled.Load()
}

// Scheduler manages tick-synchronized delayed and repeating tasks across worlds.
// Callbacks always execute safely inside the target world's transaction.
type Scheduler struct {
	mu    sync.Mutex
	tasks map[*ScheduledTask]struct{}
}

// NewScheduler creates a standalone tick Scheduler.
func NewScheduler() *Scheduler {
	return &Scheduler{tasks: make(map[*ScheduledTask]struct{})}
}

// After schedules fn to run once on world w after delayTicks simulation ticks
// (clamped to >= 1).
func (s *Scheduler) After(w *world.World, delayTicks int64, fn func(tx *world.Tx)) *ScheduledTask {
	return s.schedule(w, max(delayTicks, 1), 0, fn)
}

// Every schedules fn to run repeatedly on world w every intervalTicks
// simulation ticks (clamped to >= 1).
func (s *Scheduler) Every(w *world.World, intervalTicks int64, fn func(tx *world.Tx)) *ScheduledTask {
	interval := max(intervalTicks, 1)
	return s.schedule(w, interval, interval, fn)
}

// AfterDuration schedules fn to run once on w after approximately d (converted
// to 20 TPS ticks, minimum 1 tick).
func (s *Scheduler) AfterDuration(w *world.World, d time.Duration, fn func(tx *world.Tx)) *ScheduledTask {
	return s.After(w, durationToTicks(d), fn)
}

// EveryDuration schedules fn to run repeatedly on w every d (converted to 20 TPS
// ticks, minimum 1 tick).
func (s *Scheduler) EveryDuration(w *world.World, d time.Duration, fn func(tx *world.Tx)) *ScheduledTask {
	return s.Every(w, durationToTicks(d), fn)
}

// CancelAll cancels every active task tracked by this Scheduler.
func (s *Scheduler) CancelAll() {
	if s == nil {
		return
	}
	s.mu.Lock()
	list := make([]*ScheduledTask, 0, len(s.tasks))
	for t := range s.tasks {
		list = append(list, t)
	}
	clear(s.tasks)
	s.mu.Unlock()
	for _, t := range list {
		t.Cancel()
	}
}

// ActiveCount returns the number of currently scheduled tasks.
func (s *Scheduler) ActiveCount() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.tasks)
}

func (s *Scheduler) schedule(w *world.World, delayTicks, intervalTicks int64, fn func(tx *world.Tx)) *ScheduledTask {
	t := &ScheduledTask{}
	if s == nil || w == nil || fn == nil {
		t.cancelled.Store(true)
		return t
	}
	remaining := delayTicks
	t.onCancel = func() {
		s.mu.Lock()
		delete(s.tasks, t)
		s.mu.Unlock()
	}
	s.mu.Lock()
	s.tasks[t] = struct{}{}
	s.mu.Unlock()

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
	return t
}

func durationToTicks(d time.Duration) int64 {
	if d <= tickDuration {
		return 1
	}
	return int64((d + tickDuration/2) / tickDuration)
}
