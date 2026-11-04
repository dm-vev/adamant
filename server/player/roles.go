package player

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
)

// Permissions returns what the player may do: the level of the commands it
// runs, and its role and the abilities the role gives it, as the server
// says. A player made without a server is a member of level 0.
//
// The game holds the player to the abilities of its role, whatever its
// client shows: a refused action does nothing, silently, as on Bedrock
// Dedicated Server, and the blocks it changed on the client are sent again.
func (p *Player) Permissions() client.Permissions {
	if p.permissions == nil {
		return client.MemberPermissions()
	}
	return p.permissions(p)
}

// roleRefusesUse reports whether the role of the player forbids what using
// the item it holds on the block at pos does: opening a container needs
// AbilityOpenContainers, using a door or switch AbilityDoorsAndSwitches,
// activating any other block, using an item on it or placing one
// AbilityBuild.
func (p *Player) roleRefusesUse(pos cube.Pos) bool {
	perms := p.Permissions()
	if perms.Allows(client.AbilitiesOfMembers) {
		return false
	}
	b := p.tx.Block(pos)
	held, _ := p.HeldItems()
	_, hitDependent := b.(block.HitActivatable)
	_, activatable := b.(block.Activatable)
	if hitDependent || activatable && (!p.Sneaking() || held.Empty()) {
		switch {
		case block.IsSwitch(b):
			return !perms.Allows(client.AbilityDoorsAndSwitches)
		case block.IsContainer(b):
			return !perms.Allows(client.AbilityOpenContainers)
		}
		return !perms.Allows(client.AbilityBuild)
	}
	return !held.Empty() && !perms.Allows(client.AbilityBuild)
}

// roleRefusesEntity reports whether the role of the player forbids using
// an item on, or interacting with, the entity passed: opening an entity
// that holds items, such as a chest boat or a minecart with a chest, needs
// AbilityOpenContainers.
func (p *Player) roleRefusesEntity(e world.Entity) bool {
	return holdsItems(e) && !p.Permissions().Allows(client.AbilityOpenContainers)
}

// roleRefusesAttack reports whether the role of the player forbids
// attacking the entity passed: a player needs AbilityAttackPlayers, any
// other entity AbilityAttackMobs.
func (p *Player) roleRefusesAttack(e world.Entity) bool {
	if _, ok := e.(*Player); ok {
		return !p.Permissions().Allows(client.AbilityAttackPlayers)
	}
	return !p.Permissions().Allows(client.AbilityAttackMobs)
}

// holdsItems reports whether the entity passed, or its behaviour, holds an
// inventory a player opens.
func holdsItems(e world.Entity) bool {
	type holder interface{ Inventory() *inventory.Inventory }
	if _, ok := e.(holder); ok {
		return true
	}
	if ent, ok := e.(*entity.Ent); ok {
		_, ok := ent.Behaviour().(holder)
		return ok
	}
	return false
}
