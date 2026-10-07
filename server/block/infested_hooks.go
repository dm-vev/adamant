package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// infestedBreakHandler releases the hidden silverfish when its host block is
// mined without Silk Touch. The world registry owns the concrete mob factory,
// keeping the block package independent from entity implementations.
func infestedBreakHandler(pos cube.Pos, tx *world.Tx, user item.User) {
	if user != nil {
		main, off := user.HeldItems()
		for _, stack := range []item.Stack{main, off} {
			if _, ok := stack.Enchantment(enchantment.SilkTouch); ok {
				return
			}
		}
	}
	spawn := tx.World().EntityRegistry().Config().SpawnMob
	if spawn == nil {
		return
	}
	handle := spawn("minecraft:silverfish", world.EntitySpawnOpts{Position: pos.Vec3().Add(mgl64.Vec3{.5, 0, .5}), SpawnReason: "infested_block"})
	if handle != nil {
		tx.AddEntity(handle)
	}
}
