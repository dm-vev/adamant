package builtin

import (
	"github.com/dm-vev/adamant/server/cmd"
	"github.com/dm-vev/adamant/server/item"
	"github.com/dm-vev/adamant/server/player"
	"github.com/dm-vev/adamant/server/world"
)

type giveCommand struct {
	gameMaster
	Targets cmd.Players       `cmd:"targets"`
	Item    itemArgument      `cmd:"item"`
	Count   cmd.Optional[int] `cmd:"count"`
}

func newGiveCommand() cmd.Command {
	return newCommand("give", nil, giveCommand{})
}
func (g giveCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	it, ok := canonicalItemArgument(string(g.Item))
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
	itemName := g.Item
	cmd.ForEach(src, o, tx, g.Targets,
		func(_ *world.Tx, t cmd.Target) int {
			p, ok := t.(*player.Player)
			if !ok {
				return 0
			}
			for left := count; left > 0; left -= maxCount {
				_, _ = p.Inventory().AddItem(item.NewStack(it, min(left, maxCount)))
			}
			return 1
		},
		func(_ cmd.Source, o *cmd.Output, _ *world.Tx, rs []cmd.Result[int]) {
			tally := cmd.TallyOf(rs)
			if tally.NonZero == 0 {
				o.Error(cmd.ErrNoPlayerFound)
				return
			}
			o.SetResult(tally.Total)
			o.Printf("Gave %d [%s] to %s.", count, itemName, joinNonZeroNames(rs))
		},
	)
}

// javaItemArgument resolves a Java item argument such as "lever",
// "minecraft:oak_button" or "red_bed" to its item.
func canonicalItemArgument(id string) (world.Item, bool) {
	s, err := item.StackFromStructureNBT(map[string]any{"id": id})
	if err != nil {
		return nil, false
	}
	return s.Item(), true
}
