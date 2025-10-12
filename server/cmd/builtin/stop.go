package builtin

import (
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

type stopCommand struct {
	srv serverAdapter
}

func newStopCommand(srv serverAdapter) cmd.Command {
	return cmd.New("stop", "Stops the server.", nil, stopCommand{srv: srv})
}

func (s stopCommand) Run(src cmd.Source, o *cmd.Output, _ *world.Tx) {
	o.Print("Stopping server...")
	if err := s.srv.Close(); err != nil {
		o.Error(err)
	}
}

func (stopCommand) Allow(src cmd.Source) bool {
	_, isPlayer := src.(*player.Player)
	return !isPlayer
}
