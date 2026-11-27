package builtin

import (
	"github.com/dm-vev/adamant/server/cmd"
	"github.com/dm-vev/adamant/server/item"
	"github.com/dm-vev/adamant/server/player"
	"github.com/dm-vev/adamant/server/world"
)

type clearCommand struct {
	gameMaster
	Targets cmd.Optional[cmd.Players] `cmd:"target"`
}

func newClearCommand() cmd.Command {
	return newCommand("clear", nil, clearCommand{})
}

func (c clearCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	cmd.ForEach(src, o, tx, optionalPlayers(c.Targets, src),
		func(_ *world.Tx, t cmd.Target) int {
			p, ok := t.(*player.Player)
			if !ok {
				return 0
			}
			for range p.Inventory().Clear() {
			}
			for range p.Armour().Clear() {
			}
			p.SetHeldItems(item.Stack{}, item.Stack{})
			return 1
		},
		func(_ cmd.Source, o *cmd.Output, _ *world.Tx, rs []cmd.Result[int]) {
			tally := cmd.TallyOf(rs)
			if tally.NonZero == 0 {
				o.Error(cmd.ErrNoPlayerFound)
				return
			}
			o.SetResult(tally.Total)
			o.Printf("Cleared inventory of %s.", joinNonZeroNames(rs))
		},
	)
}
