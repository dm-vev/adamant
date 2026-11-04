package server

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/identity"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
)

// The tests of this file pin how the server tells its players apart before
// it keys them by Xbox account or by name. Each names the commit that
// changes what it pins, and those that commit changed say what they
// pinned.

// TestOfflineSameNameIsAReconnect checks that on a server keyed by name a
// client that logs in under the name of a player online, in another case
// and with another UUID, logs in as that player: the server holds it to
// its reconnect policy, which refuses it by default, and one client of the
// name is online and its operator. It was TestOfflineSameNameTwiceIsOnline-
// Twice, which pinned both clients online and both the operator.
func TestOfflineSameNameIsAReconnect(t *testing.T) {
	jt := newJoinTest(t, player.NopProvider{}, Config{})
	var err error
	if jt.srv.operators, err = loadOperators(filepath.Join(t.TempDir(), "ops.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := jt.srv.Op("Steve"); err != nil {
		t.Fatal(err)
	}
	jt.start()
	first, second := joinLogin("Steve", ""), joinLogin("steve", "")
	jt.join(t, first)
	jt.l.Send(second)
	awaitRejected(t, second, "Already logged in.")
	if n := jt.srv.PlayerCount(); n != 1 {
		t.Errorf("%d players online, want the first client of Steve alone", n)
	}
	if len(second.Games()) != 0 || len(first.Recorder().CallsTo("Disconnect")) != 0 {
		t.Error("the second client of Steve started a game, or the first was disconnected")
	}
	if !jt.srv.PlayerIsOperator(first.L.Identity.UUID) || jt.srv.PlayerIsOperator(second.L.Identity.UUID) {
		t.Error("the operator of Steve is not his first client alone")
	}
}

// TestOfflineDataFollowsTheName checks that on a server keyed by name the
// record of a player is that of its name: a client that sends the UUID of
// another player does not get its data, and a player whose client sends a
// new UUID at each login keeps its own. It was TestOfflineClientLoadsThe-
// RecordOfTheUUIDItSends, which pinned that Mallory, sending the UUID of
// Alice, played with her 1234 experience points.
func TestOfflineDataFollowsTheName(t *testing.T) {
	alice := uuid.MustParse("00000000-0000-4000-8000-0000000a11ce")
	provider := &recordProvider{records: map[uuid.UUID]player.Config{
		alice:                         {Name: "Alice", UUID: alice, Experience: 1234},
		identity.OfflineUUID("alice"): {Name: "Alice", UUID: alice, Experience: 77},
	}}
	jt := newJoinTest(t, provider, Config{})
	jt.start()
	mallory := joinLogin("Mallory", "")
	mallory.L.Identity.UUID = alice
	h := jt.join(t, mallory)
	if got := provider.loads(); len(got) != 2 || got[0] != identity.OfflineUUID("mallory") || got[1] != alice {
		t.Errorf("loaded %v, want the UUID of the name, then that the client sent", got)
	}
	xp, _ := player.Call(context.Background(), h, func(_ *world.Tx, p *player.Player) (int, error) {
		return p.Experience(), nil
	})
	if xp != 0 {
		t.Errorf("Mallory has %d experience, want none of Alice's", xp)
	}
	again := joinLogin("ALICE", "")
	h = jt.join(t, again)
	xp, _ = player.Call(context.Background(), h, func(_ *world.Tx, p *player.Player) (int, error) {
		return p.Experience(), nil
	})
	if xp != 77 {
		t.Errorf("Alice with a new UUID has %d experience, want the 77 of her name", xp)
	}
}

// TestBanKicksTheKeyedPlayer checks that BanPlayer kicks the player the
// ban is keyed by: on a server keyed by name, the player of the name in any
// case, so that /ban steve kicks Steve; on one keyed by XUID, the player of
// the account. It was TestBanKicksOnlyTheExactName, which pinned that the
// ban of steve left Steve online until he logged in again.
func TestBanKicksTheKeyedPlayer(t *testing.T) {
	for _, mode := range []identity.Mode{identity.Name, identity.XUID} {
		t.Run(mode.String(), func(t *testing.T) {
			jt := newJoinTest(t, player.NopProvider{}, Config{IdentityKey: mode})
			var err error
			if jt.srv.bans, err = loadBans(filepath.Join(t.TempDir(), "banned-players.json")); err != nil {
				t.Fatal(err)
			}
			jt.start()
			steve, alex := joinLogin("Steve", "2535412345678901"), joinLogin("Alex", "2535412345678902")
			steve.L.Identity.Authenticated, alex.L.Identity.Authenticated = true, true
			jt.join(t, steve)
			jt.join(t, alex)
			if added, err := jt.srv.BanPlayer("steve", ""); !added || err != nil {
				t.Fatalf("BanPlayer = %t, %v", added, err)
			}
			deadline := time.Now().Add(5 * time.Second)
			for len(steve.Recorder().CallsTo("Disconnect")) == 0 {
				if time.Now().After(deadline) {
					t.Fatal("the ban of steve did not kick Steve")
				}
				time.Sleep(5 * time.Millisecond)
			}
			if len(alex.Recorder().CallsTo("Disconnect")) != 0 {
				t.Error("the ban of steve kicked Alex")
			}
		})
	}
}

// TestRenamedPlayerLosesOperator pins that the operators and the whitelist
// of a server keyed by name hold names: an Xbox account that renames its
// gamertag is neither, and whoever joins under the old name is both. On a
// server keyed by XUID, TestRenamedPlayerKeepsOperator holds the other
// way.
func TestRenamedPlayerLosesOperator(t *testing.T) {
	srv := adminConfig(t, filepath.Join(t.TempDir(), "white-list.txt"), true)
	if _, err := srv.Op("Steve"); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.WhitelistAdd("Steve"); err != nil {
		t.Fatal(err)
	}
	const account, other = "2535400000000001", "2535400000000002"
	renamed := client.Identity{Name: "Bob", UUID: uuid.New(), XUID: account}
	if srv.IsOperator(renamed.Name) {
		t.Error("the account of Steve renamed to Bob is an operator")
	}
	if _, ok := srv.conf.Allower.Allow(nil, renamed, client.Profile{}); ok {
		t.Error("the account of Steve renamed to Bob is on the whitelist")
	}
	newcomer := client.Identity{Name: "Steve", UUID: uuid.New(), XUID: other}
	if _, ok := srv.conf.Allower.Allow(nil, newcomer, client.Profile{}); !ok || !srv.IsOperator(newcomer.Name) {
		t.Error("another account that took the name Steve is not his operator")
	}
}

// recordProvider is a player.Provider that holds records by UUID, records
// the UUIDs it loads, and saves nothing.
type recordProvider struct {
	player.NopProvider
	mu      sync.Mutex
	records map[uuid.UUID]player.Config
	loaded  []uuid.UUID
}

func (p *recordProvider) Load(id uuid.UUID, dim func(world.Dimension) *world.World) (player.Config, *world.World, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.loaded = append(p.loaded, id)
	if d, ok := p.records[id]; ok {
		return d, dim(world.Overworld), nil
	}
	return player.NopProvider{}.Load(id, dim)
}

func (p *recordProvider) loads() []uuid.UUID {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]uuid.UUID(nil), p.loaded...)
}
