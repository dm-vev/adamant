package server

import (
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
)

func newOnlinePlayer(name, xuid string) *onlinePlayer {
	h := world.EntitySpawnOpts{ID: uuid.New()}.New(player.Type, player.Config{Name: name})
	return &onlinePlayer{handle: h, name: name, xuid: xuid}
}

func TestPlayersRegistry(t *testing.T) {
	var r players
	if r.count() != 0 || len(r.handles()) != 0 || len(r.names()) != 0 {
		t.Fatal("the zero registry is not empty")
	}
	if _, ok := r.handle(uuid.New()); ok {
		t.Fatal("the zero registry has a player")
	}

	a, b := newOnlinePlayer("Alice", "1"), newOnlinePlayer(" Bob ", "")
	r.add(a)
	r.add(b)
	if got := r.count(); got != 2 {
		t.Fatalf("count = %d, want 2", got)
	}
	if h, ok := r.handle(a.handle.UUID()); !ok || h != a.handle {
		t.Fatal("handle does not find Alice by UUID")
	}
	if h, ok := r.byName("Alice"); !ok || h != a.handle {
		t.Fatal("byName does not find Alice")
	}
	if _, ok := r.byName("alice"); ok {
		t.Fatal("byName matches a name in another case")
	}
	if h, ok := r.byXUID("1"); !ok || h != a.handle {
		t.Fatal("byXUID does not find Alice")
	}
	if h := r.byKey("bob"); h != b.handle {
		t.Fatal("byKey does not find Bob by his normalised name")
	}
	if h := r.byKey("carol"); h != nil {
		t.Fatal("byKey finds a player that is not online")
	}
	names := r.names()
	slices.Sort(names)
	if !slices.Equal(names, []string{" Bob ", "Alice"}) {
		t.Fatalf("names = %q", names)
	}
	if hs := r.handles(); len(hs) != 2 || !slices.Contains(hs, a.handle) || !slices.Contains(hs, b.handle) {
		t.Fatal("handles does not return the handles of both players")
	}

	if !r.remove(a.handle.UUID()) {
		t.Fatal("remove reports Alice was not online")
	}
	if r.remove(a.handle.UUID()) {
		t.Fatal("remove reports Alice online after she was removed")
	}
	if _, ok := r.handle(a.handle.UUID()); ok || r.count() != 1 {
		t.Fatal("Alice is still online after remove")
	}
}
