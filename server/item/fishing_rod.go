package item

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
)

// FishingRod is a tool used to fish items and entities out of water.
type FishingRod struct{}

// MaxCount ...
func (FishingRod) MaxCount() int {
	return 1
}

// DurabilityInfo ...
func (FishingRod) DurabilityInfo() DurabilityInfo {
	return DurabilityInfo{
		MaxDurability: 64,
		BrokenItem:    simpleItem(Stack{}),
	}
}

// Use ports FishingRodItem.use: a cast hook is retrieved, wearing the rod
// down by what FishingHook.retrieve returns, otherwise a hook is cast.
func (FishingRod) Use(tx *world.Tx, user User, ctx *UseContext) bool {
	fisher, ok := user.(FishingUser)
	if !ok {
		return false
	}
	if fisher.IsFishing() {
		ctx.DamageItem(fisher.StopFishing(tx, true))
		return true
	}
	main, _ := user.HeldItems()
	if fisher.StartFishing(tx, main) {
		tx.PlaySound(user.Position(), sound.ItemThrow{})
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
