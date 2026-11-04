package builtin

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/world"
)

// stopCommand needs cmd.LevelOwner: it runs from the console, from the
// other sources of the level of owners, and from operators of level 4, as
// Java's /stop does.
type stopCommand struct {
	owner
	srv serverAdapter
}

func newStopCommand(srv serverAdapter) cmd.Command {
	return newCommand("stop", nil, stopCommand{srv: srv})
}

func (s stopCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	o.Print("Stopping server...")
	go func() {
		if err := s.srv.Close(); err != nil {
			fmt.Println("close server:", err)
		}
	}()
}
