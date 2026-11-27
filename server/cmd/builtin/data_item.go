package builtin

import (
	"github.com/dm-vev/adamant/server/cmd"
	"github.com/dm-vev/adamant/server/item"
	"github.com/dm-vev/adamant/server/item/inventory"
	"github.com/dm-vev/adamant/server/world"
)

type itemReplaceEntityCommand struct {
	gameMaster
	Targets cmd.Entities      `cmd:"targets"`
	Slot    int               `cmd:"slot"`
	Item    itemArgument      `cmd:"item"`
	Count   cmd.Optional[int] `cmd:"count"`
}

type itemModifyEntityCommand struct {
	gameMaster
	Targets cmd.Entities `cmd:"targets"`
	Slot    int          `cmd:"slot"`
	Name    string       `cmd:"name"`
}

func newItemCommand() cmd.Command {
	return newCommand("item", nil, itemReplaceEntityCommand{}, itemModifyEntityCommand{})
}

func (c itemReplaceEntityCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	if c.Slot < 0 {
		o.Errort(cmd.MessageParameterInvalid, c.Slot)
		return
	}
	it, ok := canonicalItemArgument(string(c.Item))
	if !ok {
		o.Errort(cmd.MessageParameterInvalid, c.Item)
		return
	}
	count, ok := c.Count.Load()
	if !ok {
		count = 1
	}
	maxCount := 64
	if maxCounter, ok := it.(item.MaxCounter); ok {
		maxCount = maxCounter.MaxCount()
	}
	if count < 1 || count > maxCount {
		o.Errort(cmd.MessageParameterInvalid, count)
		return
	}
	stack := item.NewStack(it, count)
	slot := c.Slot
	cmd.ForEach(src, o, tx, c.Targets,
		func(_ *world.Tx, target cmd.Target) int {
			inv, ok := target.(interface{ Inventory() *inventory.Inventory })
			if !ok {
				return 0
			}
			if err := inv.Inventory().SetItem(slot, stack); err == nil {
				return 1
			}
			return 0
		},
		func(_ cmd.Source, o *cmd.Output, _ *world.Tx, rs []cmd.Result[int]) {
			tally := cmd.TallyOf(rs)
			if tally.NonZero == 0 {
				o.Error(cmd.ErrNoEntityFound)
				return
			}
			o.SetResult(tally.Total)
			o.Printf("Replaced slot %d in %d target(s) with %s.", slot, tally.NonZero, stack.String())
		},
	)
}

func (c itemModifyEntityCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	if c.Slot < 0 {
		o.Errort(cmd.MessageParameterInvalid, c.Slot)
		return
	}
	slot, name := c.Slot, c.Name
	cmd.ForEach(src, o, tx, c.Targets,
		func(_ *world.Tx, target cmd.Target) int {
			inv, ok := target.(interface{ Inventory() *inventory.Inventory })
			if !ok {
				return 0
			}
			inventory := inv.Inventory()
			stack, err := inventory.Item(slot)
			if err != nil || stack.Empty() {
				return 0
			}
			if inventory.SetItem(slot, stack.WithCustomName(name)) == nil {
				return 1
			}
			return 0
		},
		func(_ cmd.Source, o *cmd.Output, _ *world.Tx, rs []cmd.Result[int]) {
			tally := cmd.TallyOf(rs)
			if tally.NonZero == 0 {
				o.Error(cmd.ErrNoEntityFound)
				return
			}
			o.SetResult(tally.Total)
			o.Printf("Modified slot %d in %d target(s).", slot, tally.NonZero)
		},
	)
}
