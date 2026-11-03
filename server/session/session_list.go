package session

import (
	"github.com/df-mc/dragonfly/server/client"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// The players of the clients a session lists are those of the Roster of
// its Config: the Roster lists and unlists them, in an order of its own,
// through the ListPlayer and UnlistPlayer of each session listed. A
// session lists only the players of Bedrock sessions; it shows another
// player as it shows an entity, in ViewEntity.

// roster returns the Roster of the session: that of its Config, or an
// empty one for a session made without New.
func (s *Session) roster() *client.Roster {
	if s.conf.Roster == nil {
		return new(client.Roster)
	}
	return s.conf.Roster
}

// ListPlayer lists p to the client of s: it maps the entity of a player of
// a session to a runtime ID of s, and writes its player list entry and,
// but to its own session, its abilities. A player of a client of another
// protocol is not listed. The session counts itself open from when it is
// shown itself.
func (s *Session) ListPlayer(p client.Listed) {
	o, ok := p.Client.(*Session)
	if !ok {
		return
	}
	runtimeID(o, s)
	sendSessionTo(o, s)
	if o == s {
		sessionsOpen.Add(1)
	}
}

// UnlistPlayer removes p from the player list of the client of s, and the
// runtime ID of its entity. The session counts itself closed once it is
// shown itself unlisted.
func (s *Session) UnlistPlayer(p client.Listed) {
	o, ok := p.Client.(*Session)
	if !ok {
		return
	}
	unsendSessionFrom(o, s)
	if o == s {
		sessionsOpen.Add(-1)
	}
}

// sendAbilities updates player-list permissions even for clients in another
// dimension. The payload was built on the target player's world owner.
func (s *Session) sendAbilities(data v2193.AbilityData) {
	s.roster().Update(func(players []client.Listed) {
		for _, p := range players {
			if other, ok := p.Client.(*Session); ok && other != s {
				other.writePacket(&v2193.UpdateAbilitiesPacket{AbilityData: data})
			}
		}
	})
}

// sendSessionTo writes the player list entry of s to the session to, and
// the abilities of s if to is another session.
func sendSessionTo(s, to *Session) {
	to.writePacket(s.playerListEntry())
	if data := s.abilities.Load(); data != nil && s != to {
		to.writePacket(&v2193.UpdateAbilitiesPacket{AbilityData: *data})
	}
}

// runtimeID maps the entity of s to its runtime ID in the session to, and
// returns it.
func runtimeID(s, to *Session) uint64 {
	to.entityMutex.Lock()
	defer to.entityMutex.Unlock()

	if runtimeID, ok := to.entityRuntimeIDs[s.ent]; ok {
		to.entities[runtimeID] = s.ent
		return runtimeID
	}
	runtimeID := to.runtimeIDOf(s.ent)
	to.entityRuntimeIDs[s.ent] = runtimeID
	to.entities[runtimeID] = s.ent
	return runtimeID
}

// unsendSessionFrom removes the runtime ID of the entity of s from the
// session from, and s from its player list.
func unsendSessionFrom(s, from *Session) {
	from.entityMutex.Lock()
	if id, ok := from.entityRuntimeIDs[s.ent]; ok {
		// Only remove the runtime ID when the mapping exists to avoid deleting unrelated entities.
		delete(from.entities, id)
		delete(from.entityRuntimeIDs, s.ent)
	}
	from.entityMutex.Unlock()

	from.writePacket(&v2193.PlayerListPacket{
		Entries: []v2193.PlayerListEntry{{
			ActionType: v2193.PlayerListActionRemove,
			UUID:       s.ent.UUID(),
		}},
	})
}
