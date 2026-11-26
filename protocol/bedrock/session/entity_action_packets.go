package session

import (
	"github.com/dm-vev/adamant/net/bedrock/proto"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/dm-vev/adamant/protocol/bedrock/internal/versions"
	"github.com/dm-vev/adamant/server/entity"
	"github.com/dm-vev/adamant/server/item"
	"github.com/dm-vev/adamant/server/world"
)

// ViewEntityAction shows an action performed by an entity: the actions of
// entity_action_table.go by the generated switch, and those with logic here.
func (s *Session) ViewEntityAction(e world.Entity, a world.EntityAction) {
	if s == nop || a == nil {
		return
	}
	if _, isDeath := a.(entity.DeathAction); isDeath {
		s.viewDeath(e)
		return
	}
	s.entityActionPackets(e, a, s.entityRuntimeID(e), s.writePacket)
}

// ViewEntityActionShared views an action performed by an entity using b to share
// packet construction and encoding across non-self viewers.
func (s *Session) ViewEntityActionShared(e world.Entity, a world.EntityAction, b *world.Broadcast) {
	if s == nop || a == nil {
		return
	}
	if b == nil || e.H() == s.ent || s.hasOutInterceptors.Load() {
		s.ViewEntityAction(e, a)
		return
	}
	switch act := a.(type) {
	case entity.OfferFlowerAction:
		if s.dropCosmetic() {
			return
		}
	case entity.DeathAction:
		id, visible := s.prepareDeath(e)
		if !visible {
			return
		}
		if id != e.H().RuntimeID() {
			s.writePacket(&mc.ActorEventPacket{
				EntityRuntimeID: id,
				EventType:       mc.ActorEventDeath,
			})
			return
		}
	case entity.PickedUpAction:
		if act.Collector != nil && act.Collector.H() == s.ent {
			s.ViewEntityAction(e, a)
			return
		}
	case entity.EatAction:
		if s.tables().proto != versions.Model() {
			s.ViewEntityAction(e, a)
			return
		}
	}
	s.writeBroadcast(b, func(out func(proto.Packet)) {
		s.entityActionPackets(e, a, e.H().RuntimeID(), out)
	})
}

func (s *Session) entityActionPackets(e world.Entity, a world.EntityAction, id uint64, out func(proto.Packet)) {
	if pk, ok := literalEntityActionPacket(id, a); ok {
		out(pk)
		return
	}
	switch act := a.(type) {
	case entity.SwingArmAction:
		s.swingArmPacket(e, id, out)
	case entity.HurtAction:
		out(&mc.ActorEventPacket{
			EntityRuntimeID: id,
			EventType:       mc.ActorEventHurt,
		})
		if pk := mobHealthPacket(id, e, entityTypeMetaOf(e.H().Type()), metaCapsOf(e)); pk != nil {
			out(pk)
		}
	case entity.DeathAction:
		out(&mc.ActorEventPacket{
			EntityRuntimeID: id,
			EventType:       mc.ActorEventDeath,
		})
	case entity.PickedUpAction:
		out(&mc.TakeItemActorPacket{
			ItemEntityRuntimeID:  id,
			TakerEntityRuntimeID: s.entityRuntimeID(act.Collector),
		})
	case entity.EatAction:
		s.eatActionPacket(e, id, out)
	case entity.OfferFlowerAction:
		// Bedrock plays the iron golem's flower as an animation of its
		// resource pack, which, like other animations, is cosmetic.
		if s.dropCosmetic() {
			return
		}
		out(animationPacket(id, offerFlowerAnimation))
	}
}

// swingArmPacket emits an entity swinging its arm: a player as an animation,
// except the player of the session while its own swing is being handled, any
// other entity as an event.
func (s *Session) swingArmPacket(e world.Entity, id uint64, out func(proto.Packet)) {
	if _, ok := e.(Controllable); ok {
		if id == selfEntityRuntimeID && s.swingingArm.Load() {
			return
		}
		out(&mc.AnimatePacket{
			ActionType:      mc.AnimateActionSwingArm,
			EntityRuntimeID: id,
		})
		return
	}
	out(&mc.ActorEventPacket{
		EntityRuntimeID: id,
		EventType:       mc.ActorEventStartAttacking,
	})
}

// prepareDeath records the death animation of a dying mob if currently visible
// to s and returns its runtime ID and visibility.
func (s *Session) prepareDeath(e world.Entity) (uint64, bool) {
	s.entityMutex.Lock()
	id, visible := s.entityRuntimeIDs[e.H()]
	if visible && clientRemovesDyingMob(e) {
		if s.deathAnimations == nil {
			s.deathAnimations = make(map[uint64]struct{})
		}
		s.deathAnimations[id] = struct{}{}
	}
	s.entityMutex.Unlock()
	return id, visible
}

// viewDeath shows the death of an entity the session shows, and remembers the
// animation of a mob the client removes itself at the end of it.
func (s *Session) viewDeath(e world.Entity) {
	id, visible := s.prepareDeath(e)
	if !visible {
		return
	}
	s.writePacket(&mc.ActorEventPacket{
		EntityRuntimeID: id,
		EventType:       mc.ActorEventDeath,
	})
}

// eatActionPacket emits an entity eating the consumable it holds.
func (s *Session) eatActionPacket(e world.Entity, id uint64, out func(proto.Packet)) {
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
	out(&mc.ActorEventPacket{
		EntityRuntimeID: id,
		EventType:       mc.ActorEventFeed,
		// It's a little weird how the runtime ID is still shifted 16 bits to the left here, given the
		// runtime ID already includes the meta, but it seems to work.
		EventData: (rid << 16) | int32(meta),
	})
}
