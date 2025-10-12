package builtin

import (
	"strings"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/world"
)

const defaultKickReason = "Kicked by an operator."

type kickCommand struct {
	Targets []cmd.Target              `cmd:"target"`
	Reason  cmd.Optional[cmd.Varargs] `cmd:"reason"`
}

func newKickCommand() cmd.Command {
	return cmd.New("kick", "Removes one or more players from the server.", nil, kickCommand{})
}

func (k kickCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	players := playersFromTargets(k.Targets)
	if len(players) == 0 {
		o.Errort(cmd.MessageNoTargets)
		return
	}
	reason := defaultKickReason
	if r, ok := k.Reason.Load(); ok {
		if t := strings.TrimSpace(string(r)); t != "" {
			reason = t
		}
	}
	for _, p := range players {
		p.Disconnect(reason)
	}
	o.Printf("Kicked %s", joinNames(players))
}
