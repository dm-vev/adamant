package entity

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
const goalFlagCount = 4

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

// wrappedGoal is a goal with its priority and whether it runs, as Java's
// WrappedGoal.
type wrappedGoal struct {
	goal     Goal
	priority int
	running  bool
}

func (w *wrappedGoal) canBeReplacedBy(other *wrappedGoal) bool {
	return goalIsInterruptable(w.goal) && other.priority < w.priority
}

func (w *wrappedGoal) start(m *Mob) {
	if !w.running {
		w.running = true
		if s, ok := w.goal.(goalStarter); ok {
			s.Start(m)
		}
	}
}

func (w *wrappedGoal) stop(m *Mob) {
	if w.running {
		w.running = false
		if s, ok := w.goal.(goalStopper); ok {
			s.Stop(m)
		}
	}
}

func (w *wrappedGoal) tick(m *Mob) {
	if t, ok := w.goal.(goalTicker); ok {
		t.Tick(m)
	}
}

// GoalSelector runs the goals of a mob by priority, ported from Java's
// GoalSelector.
type GoalSelector struct {
	goals    []*wrappedGoal
	locked   [goalFlagCount]*wrappedGoal
	disabled GoalFlag
}

// Add adds a goal with a priority, where a lower number goes first.
func (s *GoalSelector) Add(priority int, g Goal) {
	s.goals = append(s.goals, &wrappedGoal{goal: g, priority: priority})
}

// Remove stops and removes the goals for which f returns true.
func (s *GoalSelector) Remove(m *Mob, f func(Goal) bool) {
	kept := s.goals[:0]
	for _, w := range s.goals {
		if f(w.goal) {
			w.stop(m)
			continue
		}
		kept = append(kept, w)
	}
	clear(s.goals[len(kept):])
	s.goals = kept
}

// Running reports if a goal for which f returns true is running.
func (s *GoalSelector) Running(f func(Goal) bool) bool {
	for _, w := range s.goals {
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
	for i := range goalFlagCount {
		if w.goal.Flags()&(1<<i) == 0 {
			continue
		}
		if locked := s.locked[i]; locked != nil && !locked.canBeReplacedBy(w) {
			return false
		}
	}
	return true
}

// Tick stops the goals that may no longer run, starts the goals that may
// and ticks the goals that run, as GoalSelector.tick.
func (s *GoalSelector) Tick(m *Mob) {
	for _, w := range s.goals {
		if w.running && (w.goal.Flags()&s.disabled != 0 || !goalCanContinue(w.goal, m)) {
			w.stop(m)
		}
	}
	for i, w := range s.locked {
		if w != nil && !w.running {
			s.locked[i] = nil
		}
	}
	for _, w := range s.goals {
		if w.running || w.goal.Flags()&s.disabled != 0 || !s.canReplaceAll(w) || !w.goal.CanUse(m) {
			continue
		}
		for i := range goalFlagCount {
			if w.goal.Flags()&(1<<i) == 0 {
				continue
			}
			if locked := s.locked[i]; locked != nil {
				locked.stop(m)
			}
			s.locked[i] = w
		}
		w.start(m)
	}
	s.TickRunning(m, true)
}

// TickRunning ticks the running goals, or only those that need every tick
// unless all is set, as GoalSelector.tickRunningGoals.
func (s *GoalSelector) TickRunning(m *Mob, all bool) {
	for _, w := range s.goals {
		if w.running && (all || goalRequiresEveryTick(w.goal)) {
			w.tick(m)
		}
	}
}
