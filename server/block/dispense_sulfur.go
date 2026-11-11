package block

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/loot"
	"github.com/df-mc/dragonfly/server/world"
)

// sulfurCubeSwallowable reports whether a sulfur cube swallows the item: it is
// in #sulfur_cube_swallowable, which getDefaultDispenseMethod asks of the items
// that no behaviour is registered for and that are not equippable.
func sulfurCubeSwallowable(it world.Item) bool {
	return loot.StackInItemTag(item.NewStack(it, 1), "sulfur_cube_swallowable")
}

// dispenseToSulfurCube ports SulfurCubeBlockDispenseItemBehavior.dispenseBlock:
// the first sulfur cube in the block in front that swallows one item of the
// stack does, and the stack loses it. It reports whether one did.
func dispenseToSulfurCube(src dispenseSource) bool {
	one := src.stack.Grow(1 - src.stack.Count())
	for e := range entitiesInBlock(src.tx, src.front()) {
		if c, ok := e.(interface{ EquipItem(item.Stack) bool }); ok && c.EquipItem(one) {
			return true
		}
	}
	return false
}

// dispenseSulfurCube ports SulfurCubeBlockDispenseItemBehavior: a sulfur cube in
// front swallows the block, and it is ejected if none does.
func dispenseSulfurCube(src dispenseSource) dispensed {
	if dispenseToSulfurCube(src) {
		return src.done()
	}
	return dispenseDefault(src)
}
