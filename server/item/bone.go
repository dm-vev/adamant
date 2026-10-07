package item

import "github.com/df-mc/dragonfly/server/world"
// Bone is an item primarily obtained as a drop from skeletons and their variants.
type Bone struct{}

// EncodeItem ...
func (Bone) EncodeItem() (name string, meta int16) {
	return "minecraft:bone", 0
}
// UseOnEntity feeds or tames a wolf through its wolf interaction interface.
func (Bone) UseOnEntity(target world.Entity, tx *world.Tx, user User, ctx *UseContext) bool {
	wolf, ok := target.(interface {
		TameWithBone(*world.Tx, world.Entity) bool
	})
	if !ok || !wolf.TameWithBone(tx, user) {
		return false
	}
	ctx.SubtractFromCount(1)
	return true
}
