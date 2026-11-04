package server

import (
	"cmp"
	"net"
	"slices"
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
//   - online, the players of Online, Players, PlayerCount and the lookups
//     by UUID, name and XUID: from Accept, which spawns the player, until it leaves
//     and is saved.
//
// The zero players holds none.
type players struct {
	roster client.Roster

	mu     sync.RWMutex
	online map[uuid.UUID]*onlinePlayer
	// joins counts the players put online, so that each knows when it
	// joined among the others.
	joins uint64
}

// onlinePlayer is a player online: its entity handle and the identity its
// client logged in as.
type onlinePlayer struct {
	handle *world.EntityHandle
	id     client.Identity
	// addr is the address the client logged in from, nil if unknown: the
	// address of a proxied client its proxy signed.
	addr net.Addr
	// joined orders the players online by when they were put online.
	joined uint64
}

// add puts p online, keyed by the UUID of its handle.
func (r *players) add(p *onlinePlayer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.online == nil {
		r.online = make(map[uuid.UUID]*onlinePlayer)
	}
	r.joins++
	p.joined = r.joins
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

// player returns the player online with the UUID passed.
func (r *players) player(id uuid.UUID) (*onlinePlayer, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.online[id]
	return p, ok
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

// named returns the player online whose name is the one passed, in any
// case and with spaces around: the identity its client logged in as and
// its handle.
func (r *players) named(name string) (*onlinePlayer, bool) {
	n := normalizeName(name)
	if n == "" {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, p := range r.online {
		if normalizeName(p.id.Name) == n {
			return p, true
		}
	}
	return nil, false
}

// snapshot returns the players online, in the order they were put online.
func (r *players) snapshot() []onlinePlayer {
	r.mu.RLock()
	out := make([]onlinePlayer, 0, len(r.online))
	for _, p := range r.online {
		out = append(out, *p)
	}
	r.mu.RUnlock()
	slices.SortFunc(out, func(a, b onlinePlayer) int { return cmp.Compare(a.joined, b.joined) })
	return out
}

// handles returns the handles of the players online, in the order they
// were put online.
func (r *players) handles() []*world.EntityHandle {
	ps := r.snapshot()
	out := make([]*world.EntityHandle, len(ps))
	for i, p := range ps {
		out[i] = p.handle
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
