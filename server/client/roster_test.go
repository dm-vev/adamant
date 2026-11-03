package client_test

import (
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/client/clienttest"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
)

// rosterEvents is the log, shared by the clients of a test, of what the
// Roster showed them, in order.
type rosterEvents struct {
	mu     sync.Mutex
	events []string
}

func (e *rosterEvents) add(event string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, event)
}

func (e *rosterEvents) take() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := e.events
	e.events = nil
	return out
}

// rosterClient is a client that logs what the Roster shows it.
type rosterClient struct {
	*clienttest.Recorder
	name string
	log  *rosterEvents
}

func (c *rosterClient) ListPlayer(p client.Listed) {
	c.log.add(fmt.Sprintf("%s lists %s", c.name, p.Identity.Name))
}

func (c *rosterClient) UnlistPlayer(p client.Listed) {
	c.log.add(fmt.Sprintf("%s unlists %s", c.name, p.Identity.Name))
}

// listed returns a player of the name passed, with a client that logs to
// log.
func listed(name string, log *rosterEvents) client.Listed {
	id := uuid.New()
	return client.Listed{
		Identity: client.Identity{Name: name, UUID: id},
		Handle:   world.EntitySpawnOpts{ID: id}.New(player.Type, player.Config{Name: name}),
		Client:   &rosterClient{Recorder: clienttest.New(), name: name, log: log},
	}
}

// List shows each player listed before to the new one and the new one to
// each of them, in the order they were listed, and last the new one to
// itself; Unlist has each player that was listed unlist the player, in
// order.
func TestRosterShowsChangesInOrder(t *testing.T) {
	log := new(rosterEvents)
	var r client.Roster
	a, b, c := listed("A", log), listed("B", log), listed("C", log)

	r.List(a)
	if got, want := log.take(), []string{"A lists A"}; !slices.Equal(got, want) {
		t.Errorf("List(A): %q, want %q", got, want)
	}
	r.List(b)
	if got, want := log.take(), []string{"A lists B", "B lists A", "B lists B"}; !slices.Equal(got, want) {
		t.Errorf("List(B): %q, want %q", got, want)
	}
	r.List(c)
	if got, want := log.take(), []string{"A lists C", "C lists A", "B lists C", "C lists B", "C lists C"}; !slices.Equal(got, want) {
		t.Errorf("List(C): %q, want %q", got, want)
	}

	r.Unlist(b.Client)
	if got, want := log.take(), []string{"A unlists B", "B unlists B", "C unlists B"}; !slices.Equal(got, want) {
		t.Errorf("Unlist(B): %q, want %q", got, want)
	}
	r.Unlist(b.Client)
	if got := log.take(); len(got) != 0 {
		t.Errorf("Unlist(B) again: %q, want nothing", got)
	}

	var names []string
	for _, p := range r.Players() {
		names = append(names, p.Identity.Name)
	}
	if !slices.Equal(names, []string{"A", "C"}) {
		t.Errorf("Players() = %q, want [A C]", names)
	}
	if p, ok := r.Player(c.Handle.UUID()); !ok || p.Client != c.Client {
		t.Error("Player(C) did not return C")
	}
	if _, ok := r.Player(b.Handle.UUID()); ok {
		t.Error("Player(B) returned B after it was unlisted")
	}
}

// Player returns the player listed first of two of the same UUID.
func TestRosterPlayerReturnsTheFirstListed(t *testing.T) {
	log := new(rosterEvents)
	var r client.Roster
	first, second := listed("A", log), listed("A", log)
	second.Handle = world.EntitySpawnOpts{ID: first.Handle.UUID()}.New(player.Type, player.Config{})
	r.List(first)
	r.List(second)
	if p, ok := r.Player(first.Handle.UUID()); !ok || p.Client != first.Client {
		t.Error("Player did not return the player listed first")
	}
	r.Unlist(first.Client)
	if p, ok := r.Player(first.Handle.UUID()); !ok || p.Client != second.Client {
		t.Error("Player did not return the player listed second once the first was unlisted")
	}
}

// List waits for an Update to end.
func TestRosterUpdateExcludesList(t *testing.T) {
	log := new(rosterEvents)
	var r client.Roster
	a := listed("A", log)
	r.List(a)
	log.take()

	done := make(chan struct{})
	r.Update(func(players []client.Listed) {
		if len(players) != 1 || players[0].Client != a.Client {
			t.Errorf("Update was passed %d players, want A", len(players))
		}
		go func() {
			r.List(listed("B", log))
			close(done)
		}()
		select {
		case <-done:
			t.Error("List ran during an Update")
		case <-time.After(50 * time.Millisecond):
		}
	})
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("List did not run after the Update")
	}
}

// What each client is shown of players listed and unlisted at once, as
// Updates run too, is the list: each player listed once, and before it is
// shown unlisted.
func TestRosterConcurrentChanges(t *testing.T) {
	log := new(rosterEvents)
	var r client.Roster
	const n = 16
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p := listed(fmt.Sprintf("P%d", i), log)
			r.List(p)
			r.Update(func(players []client.Listed) {
				if !slices.ContainsFunc(players, func(o client.Listed) bool { return o.Client == p.Client }) {
					t.Errorf("Update of %s was not passed it", p.Identity.Name)
				}
			})
			_ = r.Players()
			_, _ = r.Player(p.Handle.UUID())
			r.Unlist(p.Client)
		}()
	}
	wg.Wait()
	if got := r.Players(); len(got) != 0 {
		t.Errorf("%d players listed after all were unlisted", len(got))
	}

	// For each client and player: nothing, a listing, or a listing then an
	// unlisting.
	seen := map[[2]string][]string{}
	for _, e := range log.take() {
		var who, what, whom string
		if _, err := fmt.Sscanf(e, "%s %s %s", &who, &what, &whom); err != nil {
			t.Fatal(err)
		}
		seen[[2]string{who, whom}] = append(seen[[2]string{who, whom}], what)
	}
	for pair, events := range seen {
		if !slices.Equal(events, []string{"lists"}) && !slices.Equal(events, []string{"lists", "unlists"}) {
			t.Errorf("%s was shown %s: %q", pair[0], pair[1], events)
		}
		if pair[0] == pair[1] && !slices.Equal(events, []string{"lists", "unlists"}) {
			t.Errorf("%s was shown itself %q, want listed then unlisted", pair[0], events)
		}
	}
}
