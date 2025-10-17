package builtin

import (
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

type clearCommand struct {
	Targets cmd.Optional[[]cmd.Target] `cmd:"target"`
}

func newClearCommand() cmd.Command {
	return cmd.New("clear", "Clears items from a player's inventory.", nil, clearCommand{})
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
