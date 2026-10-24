package item

import "github.com/df-mc/dragonfly/server/world"

// WarpedFungusOnAStick is an item that can be used to control saddled striders.
type WarpedFungusOnAStick struct{}

// MaxCount ...
func (WarpedFungusOnAStick) MaxCount() int {
	return 1
}

// DurabilityInfo ...
func (WarpedFungusOnAStick) DurabilityInfo() DurabilityInfo {
	return DurabilityInfo{
		MaxDurability: 100,
		BrokenItem:    simpleItem(NewStack(FishingRod{}, 1)),
	}
}

// Use boosts the strider the user steers, as FoodOnAStickItem.use does, at
// the cost of 1 durability.
func (WarpedFungusOnAStick) Use(tx *world.Tx, user User, ctx *UseContext) bool {
	if boostVehicle(tx, user, "minecraft:strider") {
		ctx.DamageItem(1)
		return true
	}
	return false
}

// EncodeItem ...
func (WarpedFungusOnAStick) EncodeItem() (name string, meta int16) {
	return "minecraft:warped_fungus_on_a_stick", 0
}
