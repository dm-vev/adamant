package entity

import (
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
)

// visibilityPercent ports LivingEntity.getVisibilityPercent for an observer
// without a matching mob head: sneaking hides 20% of the range, and
// invisibility all but what worn armour gives away.
func visibilityPercent(target world.Entity) float64 {
	v := 1.0
	if s, ok := target.(interface{ Sneaking() bool }); ok && s.Sneaking() {
		v *= .8
	}
	if i, ok := target.(interface{ Invisible() bool }); ok && i.Invisible() {
		cover := float32(0)
		if a, ok := target.(interface{ Armour() *inventory.Armour }); ok {
			for _, s := range a.Armour().Slots() {
				if !s.Empty() {
					cover += .25
				}
			}
		}
		v *= .7 * float64(max(cover, .1))
	}
	return v
}
