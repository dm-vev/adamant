package session

import (
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
)

// ViewEntityAction shows an action performed by an entity: the actions of
// entity_action_table.go by the generated switch, and those with logic here.
func (s *Session) ViewEntityAction(e world.Entity, a world.EntityAction) {
	if s.literalEntityAction(e, a) {
		return
	}
	switch act := a.(type) {
	case entity.SwingArmAction:
		s.viewSwingArm(e)
	case entity.HurtAction:
		s.writePacket(&mc.ActorEventPacket{
			EntityRuntimeID: s.entityRuntimeID(e),
			EventType:       mc.ActorEventHurt,
		})
		s.viewMobHealth(e)
	case entity.DeathAction:
		s.viewDeath(e)
	case entity.PickedUpAction:
		s.writePacket(&mc.TakeItemActorPacket{
			ItemEntityRuntimeID:  s.entityRuntimeID(e),
			TakerEntityRuntimeID: s.entityRuntimeID(act.Collector),
		})
	case entity.EatAction:
		s.viewEat(e)
	case entity.OfferFlowerAction:
		// Bedrock plays the iron golem's flower as an animation of its
		// resource pack, which, like other animations, is cosmetic.
		if s.dropCosmetic() {
			return
		}
		s.writePacket(animationPacket(s.entityRuntimeID(e), offerFlowerAnimation))
	}
}

// viewSwingArm shows an entity swinging its arm: a player as an animation,
// except the player of the session while its own swing is being handled, any
// other entity as an event.
func (s *Session) viewSwingArm(e world.Entity) {
	if _, ok := e.(Controllable); ok {
		if s.entityRuntimeID(e) == selfEntityRuntimeID && s.swingingArm.Load() {
			return
		}
		s.writePacket(&mc.AnimatePacket{
			ActionType:      mc.AnimateActionSwingArm,
			EntityRuntimeID: s.entityRuntimeID(e),
		})
		return
	}
	s.writePacket(&mc.ActorEventPacket{
		EntityRuntimeID: s.entityRuntimeID(e),
		EventType:       mc.ActorEventStartAttacking,
	})
}

// viewDeath shows the death of an entity the session shows, and remembers the
// animation of a mob the client removes itself at the end of it.
func (s *Session) viewDeath(e world.Entity) {
	s.entityMutex.Lock()
	id, visible := s.entityRuntimeIDs[e.H()]
	if visible && clientRemovesDyingMob(e) {
		if s.deathAnimations == nil {
			s.deathAnimations = make(map[uint64]struct{})
		}
		s.deathAnimations[id] = struct{}{}
	}
	s.entityMutex.Unlock()
	if !visible {
		return
	}
	s.writePacket(&mc.ActorEventPacket{
		EntityRuntimeID: id,
		EventType:       mc.ActorEventDeath,
	})
}

// viewEat shows an entity eating the consumable it holds.
func (s *Session) viewEat(e world.Entity) {
	user, ok := e.(item.User)
	if !ok {
		return
	}
	held, _ := user.HeldItems()
	it := held.Item()
	if held.Empty() {
		// This can happen sometimes if the user switches between items very quickly, so just ignore the action.
		return
	}
	if _, ok := it.(item.Consumable); !ok {
		// Not consumable, refer to the comment above.
		return
	}
	rid, meta, _ := s.tables().itemRuntimeID(it)
	s.writePacket(&mc.ActorEventPacket{
		EntityRuntimeID: s.entityRuntimeID(e),
		EventType:       mc.ActorEventFeed,
		// It's a little weird how the runtime ID is still shifted 16 bits to the left here, given the
		// runtime ID already includes the meta, but it seems to work.
		EventData: (rid << 16) | int32(meta),
	})
}
