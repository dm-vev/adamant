package block

import (
	"log/slog"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/loot"
	"github.com/df-mc/dragonfly/server/world"
)

func fillContainerLoot(tx *world.Tx, pos cube.Pos, inv *inventory.Inventory, table string, seed int64, blockName string) error {
	sequence, err := loot.RandomSequence(table)
	if err != nil {
		return err
	}
	random := tx.LootRandomSequence(sequence, seed)
	stacks, err := loot.Generate(table, seed, loot.Context{
		Random: random, RandomSequence: tx.LootRandomSequence, Tx: tx,
		Block: blockName, Origin: pos.Vec3Centre(),
	})
	if err != nil {
		return err
	}
	slots := inv.Slots()
	free := make([]int, 0, len(slots))
	for slot, stack := range slots {
		if stack.Empty() {
			free = append(free, slot)
		}
	}
	for size := len(free); size > 1; size-- {
		swap := int(random.NextInt(int32(size)))
		free[size-1], free[swap] = free[swap], free[size-1]
	}
	parts := make([]item.Stack, 0, max(len(stacks), len(free)))
	splittable := make([]item.Stack, 0, max(len(stacks), len(free)))
	for _, stack := range stacks {
		for !stack.Empty() {
			count := min(stack.Count(), stack.MaxCount())
			part := stack.Grow(count - stack.Count())
			if count > 1 {
				splittable = append(splittable, part)
			} else {
				parts = append(parts, part)
			}
			stack = stack.Grow(-count)
		}
	}
	for len(free)-len(parts)-len(splittable) > 0 && len(splittable) > 0 {
		index := 0
		if len(splittable) > 1 {
			index = int(random.NextInt(int32(len(splittable))))
		}
		stack := splittable[index]
		copy(splittable[index:], splittable[index+1:])
		splittable = splittable[:len(splittable)-1]
		remove := 1
		if bound := stack.Count() / 2; bound > 1 {
			remove += int(random.NextInt(int32(bound)))
		}
		split := stack.Grow(remove - stack.Count())
		stack = stack.Grow(-remove)
		if stack.Count() > 1 && random.NextBoolean() {
			splittable = append(splittable, stack)
		} else {
			parts = append(parts, stack)
		}
		if split.Count() > 1 && random.NextBoolean() {
			splittable = append(splittable, split)
		} else {
			parts = append(parts, split)
		}
	}
	parts = append(parts, splittable...)
	for size := len(parts); size > 1; size-- {
		swap := int(random.NextInt(int32(size)))
		parts[size-1], parts[swap] = parts[swap], parts[size-1]
	}
	for _, stack := range parts {
		if len(free) == 0 {
			slog.Warn("Tried to over-fill a container")
			return nil
		}
		slot := free[len(free)-1]
		free = free[:len(free)-1]
		if err := inv.SetItem(slot, stack); err != nil {
			return err
		}
	}
	return nil
}
