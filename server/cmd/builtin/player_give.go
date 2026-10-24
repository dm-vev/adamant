package builtin

import (
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

type giveCommand struct {
	opGate
	Targets []cmd.Target      `cmd:"targets"`
	Item    itemArgument      `cmd:"item"`
	Count   cmd.Optional[int] `cmd:"count"`
}

func newGiveCommand() cmd.Command {
	return newCommand("give", nil, giveCommand{})
}
func (g giveCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	ps := playersFromTargets(g.Targets)
	if len(ps) == 0 {
		o.Errort(cmd.MessageNoTargets)
		return
	}
	it, ok := javaItemArgument(string(g.Item))
	if !ok {
		o.Errort(cmd.MessageParameterInvalid, g.Item)
		return
	}
	count, ok := g.Count.Load()
	if !ok {
		count = 1
	}
	// Java GiveCommand allows up to 100 full stacks and splits them by the
	// item's max stack size.
	maxCount := item.NewStack(it, 1).MaxCount()
	if count < 1 || count > maxCount*100 {
		o.Errort(cmd.MessageParameterInvalid, count)
		return
	}
	for _, p := range ps {
		for left := count; left > 0; left -= maxCount {
			_, _ = p.Inventory().AddItem(item.NewStack(it, min(left, maxCount)))
		}
	}
	o.Printf("Gave %d [%s] to %s.", count, g.Item, joinNames(ps))
}

// javaItemArgument resolves a Java item argument such as "lever",
// "minecraft:oak_button" or "red_bed" to its item.
func javaItemArgument(id string) (world.Item, bool) {
	s, err := item.StackFromStructureNBT(map[string]any{"id": id})
	if err != nil {
		return nil, false
	}
	return s.Item(), true
}
