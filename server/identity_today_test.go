package server

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
)

// The tests of this file pin how the server tells its players apart before
// it keys them by Xbox account or by name. Each names the commit that
// changes what it pins.

// TestOfflineSameNameTwiceIsOnlineTwice pins that two clients that log in
// without Xbox Live under one name, in two cases and with two UUIDs, are
// both online, and both count as the operator of the name. "Key the
// players of servers without sign-in by their names" makes the second an
// "already logged in" of the first.
func TestOfflineSameNameTwiceIsOnlineTwice(t *testing.T) {
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
	jt.join(t, second)
	if n := jt.srv.PlayerCount(); n != 2 {
		t.Errorf("%d players online, want both clients of Steve", n)
	}
	if !first.Games()[0].Operator || !second.Games()[0].Operator {
		t.Error("the clients of Steve do not both count as his operator")
	}
	if len(first.Recorder().CallsTo("Disconnect")) != 0 {
		t.Error("the first client of Steve was disconnected")
	}
}

// TestOfflineClientLoadsTheRecordOfTheUUIDItSends pins that the server
// loads the record of the UUID a client sends, whatever its name: a client
// that sends the UUID of another player plays with the data of that
// player. "Key the players of servers without sign-in by their names"
// loads by the name.
func TestOfflineClientLoadsTheRecordOfTheUUIDItSends(t *testing.T) {
	alice := uuid.MustParse("00000000-0000-4000-8000-0000000a11ce")
	provider := &recordProvider{records: map[uuid.UUID]player.Config{alice: {Name: "Alice", UUID: alice, Experience: 1234}}}
	jt := newJoinTest(t, provider, Config{})
	jt.start()
	mallory := joinLogin("Mallory", "")
	mallory.L.Identity.UUID = alice
	h := jt.join(t, mallory)
	if got := provider.loads(); len(got) != 1 || got[0] != alice {
		t.Errorf("loaded %v, want the UUID Mallory sent", got)
	}
	xp, _ := player.Call(context.Background(), h, func(_ *world.Tx, p *player.Player) (int, error) {
		return p.Experience(), nil
	})
	if xp != 1234 {
		t.Errorf("Mallory has %d experience, want the 1234 of Alice", xp)
	}
}

// TestBanKicksOnlyTheExactName pins that BanPlayer looks for the player to
// kick by the name exactly as typed: /ban steve stores the ban but leaves
// Steve online until he next logs in. "Name players in admin commands by
// name or by XUID" kicks the player the ban is keyed by.
func TestBanKicksOnlyTheExactName(t *testing.T) {
	jt := newJoinTest(t, player.NopProvider{}, Config{})
	var err error
	if jt.srv.bans, err = loadBans(filepath.Join(t.TempDir(), "banned-players.json")); err != nil {
		t.Fatal(err)
	}
	jt.start()
	steve := joinLogin("Steve", "")
	h := jt.join(t, steve)
	if _, ok := jt.srv.PlayerByName("steve"); ok {
		t.Error("PlayerByName found Steve by another case")
	}
	if added, err := jt.srv.BanPlayer("steve", ""); !added || err != nil {
		t.Fatalf("BanPlayer = %t, %v", added, err)
	}
	if err := player.Do(h, func(*world.Tx, *player.Player) {}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := jt.srv.Player(steve.L.Identity.UUID); !ok || len(steve.Recorder().CallsTo("Disconnect")) != 0 {
		t.Error("the ban of steve kicked Steve")
	}
	if reason, banned := jt.srv.Banned("", "Steve"); !banned || reason != "" {
		t.Errorf("Banned(Steve) = %q, %t; want the ban stored", reason, banned)
	}
}

// TestRenamedPlayerLosesOperator pins that the operators and the whitelist
// hold names: an Xbox account that renames its gamertag is neither, and
// whoever joins under the old name is both. "Key the admin lists by Xbox
// account on servers with sign-in" keeps them with the account.
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
