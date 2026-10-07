package item

import "github.com/df-mc/dragonfly/server/world"

// Harness is an equippable, dyed harness item for compatible rideable mobs.
type Harness struct {
	Colour Colour
}

// MaxCount returns the harness stack limit.
func (Harness) MaxCount() int { return 1 }

// EncodeItem returns the Bedrock item identifier.
func (h Harness) EncodeItem() (string, int16) {
	return "minecraft:" + h.Colour.String() + "_harness", 0
}

// UseOnEntity equips the harness if the target accepts the selected colour.
func (h Harness) UseOnEntity(target world.Entity, tx *world.Tx, user User, ctx *UseContext) bool {
	mob, ok := target.(interface {
		CanEquipHarness(world.Entity) bool
		EquipHarness(*world.Tx, world.Entity, world.Item) bool
	})
	if !ok || !mob.CanEquipHarness(user) || !mob.EquipHarness(tx, user, h) {
		return false
	}
	ctx.SubtractFromCount(1)
	return true
}
