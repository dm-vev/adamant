package builtin

import (
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
)

type itemReplaceEntityCommand struct {
	opGate
	Targets []cmd.Target      `cmd:"targets"`
	Slot    int               `cmd:"slot"`
	Item    string            `cmd:"item"`
	Count   cmd.Optional[int] `cmd:"count"`
}

type itemModifyEntityCommand struct {
	opGate
	Targets []cmd.Target `cmd:"targets"`
	Slot    int          `cmd:"slot"`
	Name    string       `cmd:"name"`
}

func newItemCommand() cmd.Command {
	return cmd.New("item", "Replaces or modifies items in inventories.", nil, itemReplaceEntityCommand{}, itemModifyEntityCommand{})
}

func (c itemReplaceEntityCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	if c.Slot < 0 {
		o.Errort(cmd.MessageParameterInvalid, c.Slot)
		return
	}
	it, ok := world.ItemByName(c.Item, 0)
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
	changed := 0
	for _, target := range c.Targets {
		inv, ok := target.(interface{ Inventory() *inventory.Inventory })
		if !ok {
			continue
		}
		if err := inv.Inventory().SetItem(c.Slot, stack); err == nil {
			changed++
		}
	}
	if changed == 0 {
		o.Errort(cmd.MessageNoTargets)
		return
	}
	o.Printf("Replaced slot %d in %d target(s) with %s.", c.Slot, changed, stack.String())
}

func (c itemModifyEntityCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	if c.Slot < 0 {
		o.Errort(cmd.MessageParameterInvalid, c.Slot)
		return
	}
	changed := 0
	for _, target := range c.Targets {
		inv, ok := target.(interface{ Inventory() *inventory.Inventory })
		if !ok {
			continue
		}
		inventory := inv.Inventory()
		stack, err := inventory.Item(c.Slot)
		if err != nil || stack.Empty() {
			continue
		}
		if inventory.SetItem(c.Slot, stack.WithCustomName(c.Name)) == nil {
			changed++
		}
	}
	if changed == 0 {
		o.Errort(cmd.MessageNoTargets)
		return
	}
	o.Printf("Modified slot %d in %d target(s).", c.Slot, changed)
}
