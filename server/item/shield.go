package item

import (
	"time"

	"github.com/df-mc/dragonfly/server/item/category"
	"github.com/df-mc/dragonfly/server/world"
)

// Shield is a defensive item that can block incoming attacks when used.
type Shield struct{}

// MaxCount always returns 1.
func (Shield) MaxCount() int {
	return 1
}

// DurabilityInfo returns the durability information for the shield.
func (Shield) DurabilityInfo() DurabilityInfo {
	return DurabilityInfo{
		MaxDurability: 336,
		BrokenItem:    simpleItem(Stack{}),
	}
}

// OffHand reports that the shield may be held in the off-hand slot.
func (Shield) OffHand() bool {
	return true
}

// Category returns the creative inventory category of the shield.
func (Shield) Category() category.Category {
	return category.Equipment().WithGroup("itemGroup.name.shield")
}

// EncodeItem ...
func (Shield) EncodeItem() (name string, meta int16) {
	return "minecraft:shield", 0
}

// Release is called when a shield stops being used. Shields do not have any
// additional release behaviour, but satisfy the item.Releasable interface so
// they can respond to use and release input events.
func (Shield) Release(Releaser, *world.Tx, *UseContext, time.Duration) {}

// Requirements returns the items required to keep using the shield. Shields do
// not consume any additional resources when used.
func (Shield) Requirements() []Stack {
	return nil
}
