package builtin

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

// stopCommand needs cmd.LevelOwner, and refuses every player, as it always
// has on this server, also through /execute: it runs from the console and
// from the other sources of the level of owners.
type stopCommand struct {
	owner
	srv serverAdapter
}

func newStopCommand(srv serverAdapter) cmd.Command {
	return newCommand("stop", nil, stopCommand{srv: srv})
}

func (s stopCommand) Run(src cmd.Source, o *cmd.Output, _ *world.Tx) {
	o.Print("Stopping server...")
	go func() {
		if err := s.srv.Close(); err != nil {
			fmt.Println("close server:", err)
		}
	}()
}

func (stopCommand) Allow(src cmd.Source) bool {
	for {
		w, ok := src.(cmd.Wrapper)
		if !ok {
			break
		}
		src = w.Unwrap()
	}
	_, isPlayer := src.(*player.Player)
	return !isPlayer
}
