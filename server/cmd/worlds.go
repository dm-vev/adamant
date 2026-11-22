package cmd

import (
	"sync/atomic"

	"github.com/df-mc/dragonfly/server/world"
)

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

var worlds atomic.Pointer[Worlds]

// SetWorlds installs the worlds of the server that selectors resolve over.
// Passing nil clears the installed worlds, falling back to resolving within
// the command's transaction only.
func SetWorlds(w Worlds) {
	worlds.Store(&w)
}

// installedWorlds returns the Worlds installed with SetWorlds, if any.
func installedWorlds() (Worlds, bool) {
	w := worlds.Load()
	if w == nil || *w == nil {
		return nil, false
	}
	return *w, true
}
