package cmd

import (
	"sync/atomic"
	"time"

	"github.com/df-mc/dragonfly/server/world"
)

// defaultCrossWorldTimeout is the default time ForEach waits for remote worlds
// before cancelling pending tasks and reporting unreached targets.
const defaultCrossWorldTimeout = 5 * time.Second

// Worlds is what selectors resolve over: the worlds of a server and the
// players online in them. Its methods do not wait.
type Worlds interface {
	// Worlds returns the worlds in the order Java searches them: the
	// overworld first.
	Worlds() []*world.World
	// Online returns the handles of the players online, in the order they
	// joined.
	Online() []*world.EntityHandle
	// PlayerByName finds a player online by name, in any case.
	PlayerByName(name string) (*world.EntityHandle, bool)
}

var (
	worlds            atomic.Pointer[Worlds]
	crossWorldTimeout atomic.Int64
)

// SetWorlds installs the worlds of the server that selectors resolve over.
// Passing nil clears the installed worlds, falling back to resolving within
// the command's transaction only.
func SetWorlds(w Worlds) {
	worlds.Store(&w)
}

// SetCrossWorldTimeout sets how long cross-world selector operations wait for
// remote worlds before cancelling pending tasks and reporting unreached
// targets. A non-positive duration restores the default of 5 seconds.
func SetCrossWorldTimeout(d time.Duration) {
	if d <= 0 {
		crossWorldTimeout.Store(0)
		return
	}
	crossWorldTimeout.Store(int64(d))
}

// getCrossWorldTimeout returns the active cross-world selector timeout.
func getCrossWorldTimeout() time.Duration {
	if d := time.Duration(crossWorldTimeout.Load()); d > 0 {
		return d
	}
	return defaultCrossWorldTimeout
}

// installedWorlds returns the Worlds installed with SetWorlds, if any.
func installedWorlds() (Worlds, bool) {
	w := worlds.Load()
	if w == nil || *w == nil {
		return nil, false
	}
	return *w, true
}
