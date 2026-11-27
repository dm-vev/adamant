package builtin

import (
	"strings"

	"github.com/dm-vev/adamant/server/cmd"
	"github.com/dm-vev/adamant/server/player"
	"github.com/dm-vev/adamant/server/world"
)

const defaultKickReason = "Kicked by an operator."

type kickCommand struct {
	admin
	Targets cmd.Players               `cmd:"target"`
	Reason  cmd.Optional[cmd.Varargs] `cmd:"reason"`
}

func newKickCommand() cmd.Command {
	return newCommand("kick", nil, kickCommand{})
}

func (k kickCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	reason := defaultKickReason
	if r, ok := k.Reason.Load(); ok {
		if t := strings.TrimSpace(string(r)); t != "" {
			reason = t
		}
	}
	cmd.ForEach(src, o, tx, k.Targets,
		func(_ *world.Tx, t cmd.Target) int {
			p, ok := t.(*player.Player)
			if !ok {
				return 0
			}
			p.Disconnect(reason)
			return 1
		},
		func(_ cmd.Source, o *cmd.Output, _ *world.Tx, rs []cmd.Result[int]) {
			tally := cmd.TallyOf(rs)
			if tally.NonZero == 0 {
				o.Error(cmd.ErrNoPlayerFound)
				return
			}
			o.SetResult(tally.Total)
			o.Printf("Kicked %s", joinNonZeroNames(rs))
		},
	)
}
