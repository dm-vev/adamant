package server

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

// online puts a player of the identity passed online on srv, without a
// world: enough for the lookups by name and by XUID.
func online(srv *Server, id client.Identity) {
	handle := world.EntitySpawnOpts{ID: id.UUID}.New(player.Type, player.Config{Name: id.Name, UUID: id.UUID, XUID: id.XUID})
	srv.players.add(&onlinePlayer{handle: handle, id: id})
}

// TestResolvePlayer checks how the players an admin names to add to a list
// resolve, on a server of each mode.
func TestResolvePlayer(t *testing.T) {
	dir := t.TempDir()
	srv, _ := xuidConfig(t, filepath.Join(dir, "white-list.txt"), false)
	srv.known = srv.conf.Allower.(admission).known
	srv.known.seen("2535412345678909", "Bob", account("Bob", "2535412345678909").UUID)
	online(srv, account("Steve", steveXUID))
	byName := adminConfig(t, filepath.Join(t.TempDir(), "white-list.txt"), false)
	online(byName, client.Identity{Name: "Steve", XUID: steveXUID})

	var b strings.Builder
	for _, tc := range []struct {
		srv  *Server
		mode string
		in   string
	}{
		{srv, "xuid", "STEVE"}, {srv, "xuid", "xuid:" + steveXUID}, {srv, "xuid", "bob"}, {srv, "xuid", "xuid:2535412345678909"},
		{srv, "xuid", "Alex"}, {srv, "xuid", "xuid:12"}, {srv, "xuid", " "}, {srv, "xuid", "java:x"},
		{byName, "name", "steve"}, {byName, "name", "xuid:" + steveXUID}, {byName, "name", "Alex"},
	} {
		got, err := tc.srv.ResolvePlayer(tc.in)
		fmt.Fprintf(&b, "%s %q: %q found %d pending %t, %v\n", tc.mode, tc.in, got, got.Found, got.Pending, err)
	}
	want := `xuid "STEVE": "Steve (xuid:2535412345678901)" found 1 pending false, <nil>
xuid "xuid:2535412345678901": "Steve (xuid:2535412345678901)" found 0 pending false, <nil>
xuid "bob": "Bob (xuid:2535412345678909)" found 3 pending false, <nil>
xuid "xuid:2535412345678909": "Bob (xuid:2535412345678909)" found 0 pending false, <nil>
xuid "Alex": "Alex" found 4 pending true, <nil>
xuid "xuid:12": "xuid:12" found 0 pending false, <nil>
xuid " ": "" found 0 pending false, player name must not be empty
xuid "java:x": "" found 0 pending false, java: keys are not supported: no Java Edition account can join
name "steve": "steve" found 5 pending false, <nil>
name "xuid:2535412345678901": "" found 0 pending false, This server keys players by name (IdentityKey = "name"): name the player.
name "Alex": "Alex" found 5 pending false, <nil>
`
	if got := b.String(); got != want {
		t.Errorf("resolved:\n%s\nwant:\n%s", got, want)
	}
	if added, err := byName.Op("xuid:" + steveXUID); added || err == nil {
		t.Errorf("Op of an XUID on a server keyed by name = %t, %v", added, err)
	}
}

// TestResolveEntry checks how the entries an admin names to remove from a
// list resolve on a server keyed by XUID: by the player online, by the one
// account of the list that had the name, by the account that last joined
// with it, by the name held, and the errors of two accounts of one name and
// of a player online whose account is not on the list.
func TestResolveEntry(t *testing.T) {
	r := newAdminRecord(t)
	r.write("ops.json", `[{"xuid": "2535412345678901", "name": "Steve"}, {"xuid": "2535412345678902", "name": "Twin"}, {"xuid": "2535412345678903", "name": "twin"}, {"xuid": "2535412345678904", "name": "Old"}, "held"]`)
	srv, _ := xuidConfig(t, r.path("white-list.txt"), false)
	srv.known = srv.conf.Allower.(admission).known
	clock := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	srv.known.now = func() time.Time { return clock }
	srv.known.seen("2535412345678904", "New", account("New", "2535412345678904").UUID)
	srv.known.seen("2535412345678901", "Steve", account("Steve", steveXUID).UUID)
	online(srv, account("Online", "2535412345678901"))
	online(srv, account("Steve", "2535412345678908"))

	var b strings.Builder
	for _, in := range []string{"online", "steve", "TWIN", "old", "new", "held", "nobody", "xuid:2535412345678902"} {
		got, err := srv.ResolveEntry(Operators, in)
		fmt.Fprintf(&b, "%q: %q found %d pending %t, %v\n", in, got, got.Found, got.Pending, err)
	}
	want := `"online": "Online (xuid:2535412345678901)" found 1 pending false, <nil>
"steve": "" found 0 pending false, Steve (xuid:2535412345678908) is not an operator; the list has Steve (xuid:2535412345678901), last seen 2026-10-01: name one with xuid:<XUID>
"TWIN": "" found 0 pending false, several entries of the list had the name TWIN: Twin (xuid:2535412345678902); twin (xuid:2535412345678903): name one with xuid:<XUID>
"old": "Old (xuid:2535412345678904)" found 2 pending false, <nil>
"new": "New (xuid:2535412345678904)" found 3 pending false, <nil>
"held": "held" found 4 pending true, <nil>
"nobody": "nobody" found 4 pending true, <nil>
"xuid:2535412345678902": "Twin (xuid:2535412345678902)" found 0 pending false, <nil>
`
	if got := b.String(); got != want {
		t.Errorf("resolved:\n%s\nwant:\n%s", got, want)
	}
	if removed, err := srv.Deop("steve"); removed || err == nil {
		t.Errorf("Deop of a name online under another account = %t, %v", removed, err)
	}
	if removed, err := srv.Deop("new"); !removed || err != nil {
		t.Errorf("Deop of the name the account last joined with = %t, %v", removed, err)
	}
	if entries, _ := srv.OperatorEntries(); strings.Join(entries, ", ") != "Steve (xuid:2535412345678901), Twin (xuid:2535412345678902), twin (xuid:2535412345678903), held (held by name)" {
		t.Errorf("OperatorEntries() = %q", entries)
	}
}
