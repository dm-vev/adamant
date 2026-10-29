package session

import (
	"slices"
	"sync"

	"github.com/df-mc/dragonfly/server/internal/sliceutil"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

var sessions = new(sessionList)

type sessionList struct {
	mu   sync.Mutex
	opMu sync.Mutex
	s    []*Session
}

func (l *sessionList) Add(s *Session) {
	l.opMu.Lock()
	defer l.opMu.Unlock()

	l.mu.Lock()
	others := slices.Clone(l.s)
	l.mu.Unlock()

	// Install every runtime ID before Lookup can publish the session to ViewEntity.
	for _, other := range others {
		l.runtimeID(s, other)
		l.runtimeID(other, s)
	}
	l.runtimeID(s, s)

	l.mu.Lock()
	l.s = append(l.s, s)
	l.mu.Unlock()

	// Avoid holding the session list lock while sending packets to prevent
	// blocking unrelated session list operations on network backpressure.
	for _, other := range others {
		// Show all sessions to the new session and the new session to all
		// existing sessions.
		l.sendSessionTo(s, other)
		l.sendSessionTo(other, s)
	}
	// Show the new session to itself.
	l.sendSessionTo(s, s)
}

func (l *sessionList) Remove(s *Session, entity world.Entity) {
	l.opMu.Lock()
	l.mu.Lock()
	others := slices.Clone(l.s)
	l.s = sliceutil.DeleteVal(l.s, s)
	l.mu.Unlock()

	// Unsend outside the lock to avoid blocking list operations on I/O.
	for _, other := range others {
		l.unsendSessionFrom(s, other)
	}
	l.opMu.Unlock()

	if entity == nil {
		return
	}
	for _, other := range others {
		if other.viewLayer != nil {
			other.viewLayer.Remove(entity)
		}
	}
}

func (l *sessionList) Lookup(id uuid.UUID) (*Session, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if index := slices.IndexFunc(l.s, func(session *Session) bool {
		return session.ent.UUID() == id
	}); index != -1 {
		return l.s[index], true
	}
	return nil, false
}

// sendAbilities updates player-list permissions even for clients in another
// dimension. The payload was built on the target player's world owner.
func (l *sessionList) sendAbilities(from *Session, data v2193.AbilityData) {
	l.opMu.Lock()
	defer l.opMu.Unlock()
	l.mu.Lock()
	others := slices.Clone(l.s)
	l.mu.Unlock()
	for _, other := range others {
		if other != from {
			other.writePacket(&v2193.UpdateAbilitiesPacket{AbilityData: data})
		}
	}
}

func (l *sessionList) sendSessionTo(s, to *Session) {
	to.writePacket(s.playerListEntry())
	if data := s.abilities.Load(); data != nil && s != to {
		to.writePacket(&v2193.UpdateAbilitiesPacket{AbilityData: *data})
	}
}

// all returns the sessions of the list.
func (l *sessionList) all() []*Session {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.s)
}

func (*sessionList) runtimeID(s, to *Session) uint64 {
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

func (l *sessionList) unsendSessionFrom(s, from *Session) {
	from.entityMutex.Lock()
	if id, ok := from.entityRuntimeIDs[s.ent]; ok {
		// Only remove the runtime ID when the mapping exists to avoid deleting unrelated entities.
		delete(from.entities, id)
		delete(from.entityRuntimeIDs, s.ent)
	}
	from.entityMutex.Unlock()

	from.writePacket(&v2193.PlayerListPacket{
		Entries: []v2193.PlayerListEntry{{
			ActionType: protocol.PlayerListActionRemove,
			UUID:       s.ent.UUID(),
		}},
	})
}
