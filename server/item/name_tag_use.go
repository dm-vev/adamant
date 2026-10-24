package item

import "github.com/df-mc/dragonfly/server/world"

// UseOnEntity ports NameTagItem.interactLivingEntity: a named name tag names
// a living entity other than a player, which then stays in the world.
func (NameTag) UseOnEntity(target world.Entity, _ *world.Tx, user User, ctx *UseContext) bool {
	main, _ := user.HeldItems()
	name := main.CustomName()
	if name == "" || target.H().Type().EncodeEntity() == "minecraft:player" {
		return false
	}
	named, ok := target.(interface{ SetNameTag(string) })
	if _, living := target.(interface{ Health() float64 }); !ok || !living {
		return false
	}
	if d, ok := target.(interface{ Dead() bool }); ok && d.Dead() {
		return false
	}
	named.SetNameTag(name)
	if p, ok := target.(interface{ SetPersistenceRequired(bool) }); ok {
		p.SetPersistenceRequired(true)
	}
	ctx.SubtractFromCount(1)
	return true
}
