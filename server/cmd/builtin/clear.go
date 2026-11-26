package builtin

import (
	"github.com/dm-vev/adamant/server/cmd"
	"github.com/dm-vev/adamant/server/item"
	"github.com/dm-vev/adamant/server/player"
	"github.com/dm-vev/adamant/server/world"
)

type clearCommand struct {
	gameMaster
	Targets cmd.Optional[[]cmd.Target] `cmd:"target"`
}

func newClearCommand() cmd.Command {
	return newCommand("clear", nil, clearCommand{})
}

func (c clearCommand) Run(src cmd.Source, o *cmd.Output, _ *world.Tx) {
	targets, ok := c.Targets.Load()
	if !ok {
		if p, ok := src.(*player.Player); ok {
			targets = []cmd.Target{p}
		} else {
			o.Errort(cmd.MessageNoTargets)
			return
		}
	}

	players := playersFromTargets(targets)
	if len(players) == 0 {
		o.Errort(cmd.MessageNoTargets)
		return
	}

	for _, p := range players {
		for range p.Inventory().Clear() {
		}
		for range p.Armour().Clear() {
		}
		p.SetHeldItems(item.Stack{}, item.Stack{})
	}

	o.Printf("Cleared inventory of %s.", joinNames(players))
}
