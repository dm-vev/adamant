package entity

import (
	"math/bits"
	"slices"
)

// GoalFlag is a control a goal takes over while it runs, as Java's Goal.Flag.
// Two goals with a flag in common never run at once: the one with the lower
// priority number wins.
type GoalFlag uint8

const (
	GoalMove GoalFlag = 1 << iota
	GoalLook
	GoalJump
	GoalTarget
)

// goalFlagCount is the number of goal flags.
const (
	goalFlagCount = 4
	goalFlagMask  = GoalFlag((1 << goalFlagCount) - 1)
)

// Goal is a goal of a mob, ported from Java's Goal. Besides CanUse, a goal
// may implement the optional methods a Java goal overrides: CanContinueToUse,
// IsInterruptable, Start, Stop, RequiresUpdateEveryTick and Tick.
type Goal interface {
	// Flags returns the controls the goal takes over.
	Flags() GoalFlag
	// CanUse reports if the goal should start running.
	CanUse(m *Mob) bool
}

type (
	goalContinuer     interface{ CanContinueToUse(m *Mob) bool }
	goalInterruptable interface{ IsInterruptable() bool }
	goalStarter       interface{ Start(m *Mob) }
	goalStopper       interface{ Stop(m *Mob) }
	goalTicker        interface{ Tick(m *Mob) }
	goalEveryTick     interface{ RequiresUpdateEveryTick() bool }
)

// goalFlags may be embedded by goals to hold their flags.
type goalFlags struct{ flags GoalFlag }

// Flags ...
func (g *goalFlags) Flags() GoalFlag { return g.flags }

// setFlags replaces the flags of the goal, as Goal.setFlags.
func (g *goalFlags) setFlags(f GoalFlag) { g.flags = f }

// reducedTickDelay halves a tick delay for goals updated every other tick,
// as Goal.reducedTickDelay.
func reducedTickDelay(ticks int) int {
	return (ticks + 1) / 2
}

// adjustedTickDelay returns ticks for a goal updated every tick and the
// reduced delay otherwise, as Goal.adjustedTickDelay.
func adjustedTickDelay(g Goal, ticks int) int {
	if goalRequiresEveryTick(g) {
		return ticks
	}
	return reducedTickDelay(ticks)
}

func goalCanContinue(g Goal, m *Mob) bool {
	if c, ok := g.(goalContinuer); ok {
		return c.CanContinueToUse(m)
	}
	return g.CanUse(m)
}

func goalIsInterruptable(g Goal) bool {
	if i, ok := g.(goalInterruptable); ok {
		return i.IsInterruptable()
	}
	return true
}

func goalRequiresEveryTick(g Goal) bool {
	if e, ok := g.(goalEveryTick); ok {
		return e.RequiresUpdateEveryTick()
	}
	return false
}

// wrappedGoal is a goal with its priority, pre-resolved optional hooks and
// flags, and whether it runs, as Java's WrappedGoal.
type wrappedGoal struct {
	goal        Goal
	continuer   goalContinuer
	interrupter goalInterruptable
	starter     goalStarter
	stopper     goalStopper
	ticker      goalTicker
	everyTicker goalEveryTick
	priority    int
	flags       GoalFlag
	running     bool
}

func (w *wrappedGoal) canContinue(m *Mob) bool {
	if w.continuer != nil {
		return w.continuer.CanContinueToUse(m)
	}
	return w.goal.CanUse(m)
}

func (w *wrappedGoal) isInterruptable() bool {
	if w.interrupter != nil {
		return w.interrupter.IsInterruptable()
	}
	return true
}

func (w *wrappedGoal) requiresEveryTick() bool {
	return w.everyTicker != nil && w.everyTicker.RequiresUpdateEveryTick()
}

func (w *wrappedGoal) canBeReplacedBy(other *wrappedGoal) bool {
	return w.isInterruptable() && other.priority < w.priority
}

func (w *wrappedGoal) start(m *Mob) {
	if !w.running {
		w.running = true
		if w.starter != nil {
			w.starter.Start(m)
		}
	}
}

func (w *wrappedGoal) stop(m *Mob) {
	if w.running {
		w.running = false
		if w.stopper != nil {
			w.stopper.Stop(m)
		}
	}
}

func (w *wrappedGoal) tick(m *Mob) {
	if w.ticker != nil {
		w.ticker.Tick(m)
	}
}

// GoalSelector runs the goals of a mob by priority, ported from Java's
// GoalSelector.
type GoalSelector struct {
	goals          []*wrappedGoal
	runningIndices []uint16
	locked         [goalFlagCount]*wrappedGoal
	runningFlags   GoalFlag
	disabled       GoalFlag
}

// Add adds a goal with a priority, where a lower number goes first.
func (s *GoalSelector) Add(priority int, g Goal) {
	w := &wrappedGoal{
		goal:     g,
		priority: priority,
		flags:    g.Flags(),
	}
	w.continuer, _ = g.(goalContinuer)
	w.interrupter, _ = g.(goalInterruptable)
	w.starter, _ = g.(goalStarter)
	w.stopper, _ = g.(goalStopper)
	w.ticker, _ = g.(goalTicker)
	w.everyTicker, _ = g.(goalEveryTick)
	s.goals = append(s.goals, w)
}

// Remove stops and removes the goals for which f returns true.
func (s *GoalSelector) Remove(m *Mob, f func(Goal) bool) {
	kept := s.goals[:0]
	s.runningIndices = s.runningIndices[:0]
	s.runningFlags = 0
	for _, w := range s.goals {
		if f(w.goal) {
			s.clearLocked(w)
			w.stop(m)
			continue
		}
		if w.running {
			s.runningIndices = append(s.runningIndices, uint16(len(kept)))
			s.runningFlags |= w.flags
		}
		kept = append(kept, w)
	}
	clear(s.goals[len(kept):])
	s.goals = kept
}

// Running reports if a goal for which f returns true is running.
func (s *GoalSelector) Running(f func(Goal) bool) bool {
	for _, idx := range s.runningIndices {
		w := s.goals[idx]
		if w.running && f(w.goal) {
			return true
		}
	}
	return false
}

// SetControlFlag enables or disables the goals that take over a flag.
func (s *GoalSelector) SetControlFlag(flag GoalFlag, enabled bool) {
	if enabled {
		s.disabled &^= flag
	} else {
		s.disabled |= flag
	}
}

func (s *GoalSelector) canReplaceAll(w *wrappedGoal) bool {
	overlap := w.flags & s.runningFlags & goalFlagMask
	if overlap == 0 {
		return true
	}
	for overlap != 0 {
		i := bits.TrailingZeros8(uint8(overlap))
		overlap &^= 1 << i
		if locked := s.locked[i]; locked != nil && !locked.canBeReplacedBy(w) {
			return false
		}
	}
	return true
}

func (s *GoalSelector) clearLocked(w *wrappedGoal) {
	for mask := w.flags & goalFlagMask; mask != 0; {
		i := bits.TrailingZeros8(uint8(mask))
		mask &^= 1 << i
		if s.locked[i] == w {
			s.locked[i] = nil
		}
	}
	s.runningFlags &^= w.flags
}

func (s *GoalSelector) stopRunning(w *wrappedGoal, m *Mob) {
	s.clearLocked(w)
	w.stop(m)
	for i, idx := range s.runningIndices {
		if s.goals[idx] == w {
			s.runningIndices = slices.Delete(s.runningIndices, i, i+1)
			break
		}
	}
}

func (s *GoalSelector) insertRunningIndex(idx uint16) {
	pos := len(s.runningIndices)
	for pos > 0 && s.runningIndices[pos-1] > idx {
		pos--
	}
	if pos == len(s.runningIndices) {
		s.runningIndices = append(s.runningIndices, idx)
		return
	}
	if s.runningIndices[pos] == idx {
		return
	}
	s.runningIndices = slices.Insert(s.runningIndices, pos, idx)
}

// Tick stops the goals that may no longer run, starts the goals that may
// and ticks the goals that run, as GoalSelector.tick.
func (s *GoalSelector) Tick(m *Mob) {
	kept := s.runningIndices[:0]
	for _, idx := range s.runningIndices {
		w := s.goals[idx]
		if !w.running {
			continue
		}
		if w.flags&s.disabled != 0 || !w.canContinue(m) {
			s.clearLocked(w)
			w.stop(m)
			continue
		}
		kept = append(kept, idx)
	}
	s.runningIndices = kept
	for i := 0; i < len(s.goals); i++ {
		w := s.goals[i]
		if w.running || w.flags&s.disabled != 0 || !s.canReplaceAll(w) || !w.goal.CanUse(m) {
			continue
		}
		for mask := w.flags & goalFlagMask; mask != 0; {
			bit := bits.TrailingZeros8(uint8(mask))
			mask &^= 1 << bit
			if locked := s.locked[bit]; locked != nil && locked != w {
				s.stopRunning(locked, m)
			}
			s.locked[bit] = w
		}
		s.runningFlags |= w.flags
		s.insertRunningIndex(uint16(i))
		w.start(m)
	}
	s.TickRunning(m, true)
}

// TickRunning ticks the running goals, or only those that need every tick
// unless all is set, as GoalSelector.tickRunningGoals.
func (s *GoalSelector) TickRunning(m *Mob, all bool) {
	for _, idx := range s.runningIndices {
		w := s.goals[idx]
		if w.running && (all || w.requiresEveryTick()) {
			w.tick(m)
		}
	}
}
