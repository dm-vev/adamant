package client

import (
	"slices"
	"sync"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
)

// Roster is the list of the players that a server lists to the clients of
// its players: those whose client was bound and has not yet closed, in the
// order they were listed. The server keeps one and hands it to every
// client in its Game. The adapter of each client lists and unlists its own
// player on it, and the Roster shows each change to the clients of the
// players listed, through PlayerList. The zero Roster is an empty list.
//
// List, Unlist and Update run one at a time. What every client listed is
// shown of the list therefore is the list: each player listed exactly
// once, and before it is shown that player unlisted. They hold the Roster
// while they call the clients, which must therefore not block on the
// network, nor call List, Unlist or Update from those calls.
type Roster struct {
	op      sync.Mutex // serialises List, Unlist and Update
	mu      sync.Mutex // guards players
	players []Listed
}

// Listed is a player listed on a Roster.
type Listed struct {
	// Identity is who the client of the player logged in as.
	Identity Identity
	// Handle is the entity handle of the player.
	Handle *world.EntityHandle
	// Client is the client of the player.
	Client Joining
}

// PlayerList shows a client the players of the Roster of its Game, as its
// protocol lists players.
type PlayerList interface {
	// ListPlayer lists p to the client: p is listed on the Roster. The
	// Roster calls it with the order its List documents.
	ListPlayer(p Listed)
	// UnlistPlayer removes p from the list of the client: p was unlisted
	// from the Roster.
	UnlistPlayer(p Listed)
}

// List lists p. It adds p to the list, then, for each player o listed
// before p in order, shows p to the client of o (o.Client.ListPlayer(p))
// and o to the client of p (p.Client.ListPlayer(o)). Last, it shows p to
// its own client.
func (r *Roster) List(p Listed) {
	r.op.Lock()
	defer r.op.Unlock()

	r.mu.Lock()
	others := slices.Clone(r.players)
	r.players = append(r.players, p)
	r.mu.Unlock()

	for _, o := range others {
		o.Client.ListPlayer(p)
		p.Client.ListPlayer(o)
	}
	p.Client.ListPlayer(p)
}

// Unlist unlists the player of the client c, if it is listed. It removes
// the player from the list, then has the client of each player that was
// listed, c among them, unlist it, in the order they were listed.
func (r *Roster) Unlist(c Joining) {
	r.op.Lock()
	defer r.op.Unlock()

	r.mu.Lock()
	i := slices.IndexFunc(r.players, func(l Listed) bool { return l.Client == c })
	if i < 0 {
		r.mu.Unlock()
		return
	}
	listed := slices.Clone(r.players)
	p := r.players[i]
	r.players = slices.Delete(r.players, i, i+1)
	r.mu.Unlock()

	for _, o := range listed {
		o.Client.UnlistPlayer(p)
	}
}

// Players returns the players listed, in the order they were listed.
func (r *Roster) Players() []Listed {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.players)
}

// Player returns the player listed with the UUID passed, if any: the one
// listed first if two are.
func (r *Roster) Player(id uuid.UUID) (Listed, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range r.players {
		if p.Handle.UUID() == id {
			return p, true
		}
	}
	return Listed{}, false
}

// Update runs f with the players listed, while no player is listed or
// unlisted. What f shows the clients of those players then reaches every
// client listed exactly once. f must not call List, Unlist or Update.
func (r *Roster) Update(f func(players []Listed)) {
	r.op.Lock()
	defer r.op.Unlock()
	f(r.Players())
}
