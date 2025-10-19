package item

import "github.com/df-mc/dragonfly/server/item/category"

// Shield is a defensive item that can block incoming attacks when used.
type Shield struct{}

// MaxCount always returns 1.
func (Shield) MaxCount() int {
	return 1
}

// RepairableBy reports if the provided stack can repair the shield.
func (Shield) RepairableBy(i Stack) bool {
	if planks, ok := i.Item().(interface{ RepairsWoodTools() bool }); ok {
		return planks.RepairsWoodTools()
	}
	return false
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
