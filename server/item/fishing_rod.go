package item

import "github.com/df-mc/dragonfly/server/world"

// FishingRod is a tool used to fish items and entities out of water.
type FishingRod struct{}

// MaxCount ...
func (FishingRod) MaxCount() int {
	return 1
}

// DurabilityInfo ...
func (FishingRod) DurabilityInfo() DurabilityInfo {
	return DurabilityInfo{
		MaxDurability: 65,
		BrokenItem:    simpleItem(Stack{}),
	}
}

// Use toggles fishing: If the user is already fishing the hook is reeled in, otherwise a hook is cast.
func (FishingRod) Use(tx *world.Tx, user User, ctx *UseContext) bool {
	fisher, ok := user.(FishingUser)
	if !ok {
		return false
	}

	if fisher.IsFishing() {
		if fisher.StopFishing(tx, true) {
			ctx.DamageItem(1)
		}
		return true
	}

	main, _ := user.HeldItems()
	if fisher.StartFishing(tx, main) {
		ctx.DamageItem(1)
		return true
	}
	return false
}

// EnchantmentValue ...
func (FishingRod) EnchantmentValue() int {
	return 1
}

// EncodeItem ...
func (FishingRod) EncodeItem() (name string, meta int16) {
	return "minecraft:fishing_rod", 0
}
