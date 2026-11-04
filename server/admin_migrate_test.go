package server

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/identity"
	"github.com/google/uuid"
)

// fakeLister lists the records it holds, as playerdb.Provider lists its.
type fakeLister []struct {
	id         uuid.UUID
	name, xuid string
}

func (l fakeLister) Players(yield func(id uuid.UUID, name, xuid string) bool) error {
	for _, r := range l {
		if !yield(r.id, r.name, r.xuid) {
			break
		}
	}
	return nil
}

// TestNamesMoveToXUIDsAtFirstStart pins the move of the names on the lists
// to Xbox accounts at the first start of a server keyed by XUID: a name one
// saved record gives to an account moves, in each list; a name two records
// give to two accounts, and a name no record gives, stay held by name; a
// name whose account is on the list already is dropped; the files are kept
// as *.before-xuid first, and known-players.json is written. A second start
// moves nothing and writes nothing.
func TestNamesMoveToXUIDsAtFirstStart(t *testing.T) {
	r := newAdminRecord(t)
	r.write("white-list.txt", "Steve\r\nalex\r\nxuid:2535412345678902 Alex\r\nambiguous\r\nunknown\r\n")
	r.write("ops.json", "[\"steve\", \"ambiguous\"]\n")
	r.write("banned-players.json", `{"players": [{"name": "griefer", "reason": "griefing"}, {"name": "unknown"}], "ips": [{"ip": "192.0.2.8"}]}`)
	files := []string{"white-list.txt", "ops.json", "banned-players.json", "white-list.txt" + backupSuffix, "ops.json" + backupSuffix, "banned-players.json" + backupSuffix, "known-players.json"}
	r.step("before the first start", files...)

	lister := fakeLister{
		{uuid.MustParse("00000000-0000-4000-8000-000000000001"), "Steve", "2535412345678901"},
		{uuid.MustParse("00000000-0000-4000-8000-000000000002"), "Alex", "2535412345678902"},
		{uuid.MustParse("00000000-0000-4000-8000-000000000003"), "Ambiguous", "2535412345678903"},
		{uuid.MustParse("00000000-0000-4000-8000-000000000004"), "AMBIGUOUS", "2535412345678904"},
		{uuid.MustParse("00000000-0000-4000-8000-000000000005"), "Griefer", "2535412345678905"},
		{uuid.MustParse("00000000-0000-4000-8000-000000000006"), "Unknown", ""},
		{uuid.MustParse("00000000-0000-4000-8000-000000000007"), "steve", "2535412345678901"},
		{uuid.MustParse("00000000-0000-4000-8000-000000000008"), "Bad", "not an xuid"},
	}
	var logged bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{ReplaceAttr: noTime}))
	wl, err := LoadWhitelist(r.path("white-list.txt"))
	if err != nil {
		t.Fatal(err)
	}
	ops, err := loadOperators(r.path("ops.json"))
	if err != nil {
		t.Fatal(err)
	}
	bans, err := loadBans(r.path("banned-players.json"))
	if err != nil {
		t.Fatal(err)
	}
	known, existed, err := loadKnownPlayers(r.path("known-players.json"), log)
	if err != nil || existed {
		t.Fatalf("known players before the first start: %t, %v", existed, err)
	}
	a := admission{mode: identity.XUID, log: log, whitelist: wl, operators: ops, bans: bans, known: known}
	if err := moveToAccounts(a, lister); err != nil {
		t.Fatal(err)
	}
	r.step("after the move", files...)
	if p, ok := known.named("ambiguous"); ok {
		t.Errorf("the ambiguous name is the account of %+v", p)
	}
	r.note("-- log\n%s", logged.String())

	logged.Reset()
	srv, second := xuidConfig(t, r.path("white-list.txt"), true)
	r.step("after a second start", files...)
	r.note("-- log\n%s", second.String())
	_ = srv
	r.compare("xuid/migration")
}

// TestNoMoveOnAFreshServer checks that the first start of a server keyed by
// XUID with empty lists keeps no copies of them and only writes
// known-players.json.
func TestNoMoveOnAFreshServer(t *testing.T) {
	dir := t.TempDir()
	_, logged := xuidConfig(t, filepath.Join(dir, "white-list.txt"), false)
	names, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, n := range names {
		got = append(got, n.Name())
	}
	if want := []string{"banned-players.json", "known-players.json", "ops.json", "white-list.txt"}; !slices.Equal(got, want) {
		t.Errorf("files %q, want %q", got, want)
	}
	if logged.Len() != 0 {
		t.Errorf("logged\n%s", logged.String())
	}
}

// TestMoveOfAnAccountOfTwoNames checks that a name moves to its account
// when the records hold the account under two names, as those of a player
// who renamed and whose client sent another UUID: each name on the lists
// becomes the entry of the account, shown as its record names it.
func TestMoveOfAnAccountOfTwoNames(t *testing.T) {
	r := newAdminRecord(t)
	r.write("white-list.txt", "steve\r\n")
	r.write("ops.json", "[\"bob\"]\n")
	wl, _ := LoadWhitelist(r.path("white-list.txt"))
	ops, _ := loadOperators(r.path("ops.json"))
	bans, _ := loadBans(r.path("banned-players.json"))
	known, _, _ := loadKnownPlayers(r.path("known-players.json"), nil)
	a := admission{mode: identity.XUID, log: slog.New(slog.DiscardHandler), whitelist: wl, operators: ops, bans: bans, known: known}
	lister := fakeLister{
		{uuid.MustParse("00000000-0000-4000-8000-000000000001"), "Steve", steveXUID},
		{uuid.MustParse("00000000-0000-4000-8000-000000000002"), "Bob", steveXUID},
	}
	if err := moveToAccounts(a, lister); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(r.path("white-list.txt")); string(data) != "xuid:"+steveXUID+" Steve\r\n" {
		t.Errorf("white-list.txt: %q", data)
	}
	k, _ := identity.OfXUID(steveXUID)
	if !ops.hasKey(k) {
		t.Error("the operator bob did not move to the account")
	}
}
