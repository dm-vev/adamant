package server

import (
	"sync"

	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
)

// players is the registry of the players of a server that clients play.
// It keeps them in two lists, of different lifetimes:
//   - roster, the players every client lists: from the Bind of the client
//     of a player, as the server makes the player, until the client
//     unlists it, last in its Close. The adapters of the clients read it
//     to replicate it.
//   - online, the players of Players, PlayerCount and the lookups by UUID,
//     name and XUID: from Accept, which spawns the player, until it leaves
//     and is saved.
//
// The zero players holds none.
type players struct {
	roster client.Roster

	mu     sync.RWMutex
	online map[uuid.UUID]*onlinePlayer
}

// onlinePlayer is a player online: its entity handle and the identity its
// client logged in as.
type onlinePlayer struct {
	handle *world.EntityHandle
	id     client.Identity
}

// add puts p online, keyed by the UUID of its handle.
func (r *players) add(p *onlinePlayer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.online == nil {
		r.online = make(map[uuid.UUID]*onlinePlayer)
	}
	r.online[p.handle.UUID()] = p
}

// remove takes the player of the UUID passed offline. It reports whether
// the player was online.
func (r *players) remove(id uuid.UUID) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.online[id]
	delete(r.online, id)
	return ok
}

// handle returns the handle of the player online with the UUID passed.
func (r *players) handle(id uuid.UUID) (*world.EntityHandle, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.online[id]
	if !ok {
		return nil, false
	}
	return p.handle, true
}

// byName returns the handle of a player online whose name is exactly the
// one passed.
func (r *players) byName(name string) (*world.EntityHandle, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, p := range r.online {
		if p.id.Name == name {
			return p.handle, true
		}
	}
	return nil, false
}

// byXUID returns the handle of a player online with the XUID passed.
func (r *players) byXUID(xuid string) (*world.EntityHandle, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, p := range r.online {
		if p.id.XUID == xuid {
			return p.handle, true
		}
	}
	return nil, false
}

// byKey returns the handle of a player online whose name normalizeName
// makes the key passed, nil if none is online.
func (r *players) byKey(key string) *world.EntityHandle {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, p := range r.online {
		if normalizeName(p.id.Name) == key {
			return p.handle
		}
	}
	return nil
}

// handles returns the handles of the players online, in no order.
func (r *players) handles() []*world.EntityHandle {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*world.EntityHandle, 0, len(r.online))
	for _, p := range r.online {
		out = append(out, p.handle)
	}
	return out
}

// names returns the names of the players online, in no order.
func (r *players) names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.online))
	for _, p := range r.online {
		out = append(out, p.id.Name)
	}
	return out
}

// count returns the number of players online.
func (r *players) count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.online)
}
