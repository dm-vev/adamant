package server

import (
	"bytes"
	"fmt"
	"log/slog"
	"net"
	"path/filepath"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/identity"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/google/uuid"
)

// The tests of this file hold the admin lists of a server keyed by XUID:
// the files it writes, in goldens of testdata/admin/xuid, the admission,
// and the pending names that bind to the first account that joins with
// them.

const (
	steveXUID = "2535412345678901"
	alexXUID  = "2535412345678902"
	otherXUID = "2535412345678903"
)

// account returns the identity of a player Xbox Live vouched for.
func account(name, xuid string) client.Identity {
	return client.Identity{Name: name, XUID: xuid, UUID: uuid.NewMD5(uuid.Nil, []byte(xuid)), Authenticated: true}
}

// xuidConfig returns the Server of the admin lists UserConfig.Config makes
// of a whitelist at the path passed for a server keyed by XUID, and what
// it logged.
func xuidConfig(t *testing.T, whitelist string, enabled bool) (*Server, *bytes.Buffer) {
	t.Helper()
	uc := DefaultConfig()
	uc.World.SaveData, uc.Players.SaveData = false, false
	uc.Whitelist.File, uc.Whitelist.Enabled = whitelist, enabled
	uc.Server.IdentityKey = "xuid"
	var logged bytes.Buffer
	conf, err := uc.Config(slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{ReplaceAttr: noTime})))
	if err != nil {
		t.Fatal(err)
	}
	return adminServer(t, conf), &logged
}

// noTime drops the time of the lines a test logs.
func noTime(_ []string, a slog.Attr) slog.Attr {
	if a.Key == slog.TimeKey {
		return slog.Attr{}
	}
	return a
}

// TestAdminFilesXUID pins what a server keyed by XUID writes to its lists
// as an admin names players by XUID and by name, and as players bind the
// names held for them by joining.
func TestAdminFilesXUID(t *testing.T) {
	for _, name := range []string{"white-list.txt", "white-list.toml"} {
		t.Run(name, func(t *testing.T) {
			r := newAdminRecord(t)
			srv, logged := xuidConfig(t, r.path(name), true)
			files := []string{name, "ops.json", "banned-players.json"}
			for _, s := range []struct {
				desc string
				f    func() (bool, error)
			}{
				{`WhitelistAdd("xuid:` + steveXUID + `")`, func() (bool, error) { return srv.WhitelistAdd("xuid:" + steveXUID) }},
				{`WhitelistAdd(" Alex Two ")`, func() (bool, error) { return srv.WhitelistAdd(" Alex Two ") }},
				{`WhitelistAdd("Alex")`, func() (bool, error) { return srv.WhitelistAdd("Alex") }},
				{`WhitelistAdd("xuid:00` + steveXUID + `")`, func() (bool, error) { return srv.WhitelistAdd("xuid:00" + steveXUID) }},
				{`Op("Alex")`, func() (bool, error) { return srv.Op("Alex") }},
				{`Op("xuid:` + otherXUID + `")`, func() (bool, error) { return srv.Op("xuid:" + otherXUID) }},
				{`BanPlayer("Griefer", "griefing")`, func() (bool, error) { return srv.BanPlayer("Griefer", "griefing") }},
				{`BanPlayer("xuid:` + otherXUID + `", "")`, func() (bool, error) { return srv.BanPlayer("xuid:"+otherXUID, "") }},
			} {
				ok, err := s.f()
				r.step(fmt.Sprintf("%s = %t, %v", s.desc, ok, err), files...)
			}
			for _, j := range []struct {
				desc string
				id   client.Identity
			}{
				{"Alex joins", account("ALEX", alexXUID)},
				{"Steve joins", account("Steve", steveXUID)},
				{"Griefer joins", account("Griefer", "2535412345678904")},
				{"another account joins as Alex", account("Alex", "2535412345678905")},
			} {
				msg, ok := srv.conf.Allower.Allow(nil, j.id, client.Profile{})
				r.step(fmt.Sprintf("%s (xuid:%s): %t %q", j.desc, j.id.XUID, ok, msg), files...)
			}
			for _, s := range []struct {
				desc string
				f    func() (bool, error)
			}{
				{`WhitelistRemove("Alex Two")`, func() (bool, error) { return srv.WhitelistRemove("Alex Two") }},
				{`WhitelistRemove("xuid:` + alexXUID + `")`, func() (bool, error) { return srv.WhitelistRemove("xuid:" + alexXUID) }},
				{`Deop("alex")`, func() (bool, error) { return srv.Deop("alex") }},
				{`PardonPlayer("griefer")`, func() (bool, error) { return srv.PardonPlayer("griefer") }},
			} {
				ok, err := s.f()
				r.step(fmt.Sprintf("%s = %t, %v", s.desc, ok, err), files...)
			}
			entries, _ := srv.WhitelistEntries()
			ops, _ := srv.OperatorEntries()
			bans, _ := srv.BanEntries(false)
			r.note("WhitelistEntries() = %q\nOperatorEntries() = %q\nBanEntries(false) = %q", entries, ops, bans)
			r.note("-- log\n%s", logged.String())
			r.compare("xuid/" + strings.ReplaceAll(name, ".", "_"))
		})
	}
}

// TestAdminFilesMixed pins how files that hold entries of both kinds load
// on a server of each mode, and what a server keyed by name writes back
// after a change: the entries of accounts as they were.
func TestAdminFilesMixed(t *testing.T) {
	for _, mode := range []string{"name", "xuid"} {
		t.Run(mode, func(t *testing.T) {
			r := newAdminRecord(t)
			r.write("white-list.txt", "xuid:"+steveXUID+" Steve\r\nalex\r\nXUID:0002535412345678902\r\nxuid:12a not an account\r\nxuid:"+otherXUID+"   Alex  Two  \r\n")
			r.write("ops.json", `["pending", {"xuid": "`+steveXUID+`", "name": "Steve"}, {"xuid": "`+alexXUID+`", "name": ""}]`)
			r.write("banned-players.json", `{"players": [{"xuid": "`+otherXUID+`", "name": "Griefer", "reason": "griefing"}, {"name": "Pending"}], "ips": []}`)
			files := []string{"white-list.txt", "ops.json", "banned-players.json"}
			uc := DefaultConfig()
			uc.World.SaveData, uc.Players.SaveData = false, false
			uc.Whitelist.File, uc.Whitelist.Enabled = r.path("white-list.txt"), true
			uc.Server.IdentityKey = mode
			var logged bytes.Buffer
			conf, err := uc.Config(slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{ReplaceAttr: noTime})))
			if err != nil {
				t.Fatal(err)
			}
			srv := adminServer(t, conf)
			r.step("loaded", files...)
			entries, _ := srv.WhitelistEntries()
			ops, _ := srv.OperatorEntries()
			bans, _ := srv.BanEntries(false)
			r.note("WhitelistEntries() = %q\nOperatorEntries() = %q\nBanEntries(false) = %q", entries, ops, bans)
			for _, id := range []client.Identity{
				account("STEVE", steveXUID), account("Alex Two", otherXUID), account("Griefer", "2535412345678906"),
				{Name: "Steve", XUID: steveXUID}, {Name: "Pending"}, account("alex", "2535412345678907"),
			} {
				msg, ok := srv.conf.Allower.Allow(nil, id, client.Profile{})
				r.note("Allow(%q xuid %q authenticated %t) = %t %q; operator %t", id.Name, id.XUID, id.Authenticated, ok, msg, srv.operatorOf(id))
			}
			_, err = srv.WhitelistAdd("New")
			r.note("WhitelistAdd(\"New\"): %v", err)
			_, err = srv.Op("New")
			r.note("Op(\"New\"): %v", err)
			_, err = srv.BanPlayer("New", "")
			r.note("BanPlayer(\"New\", \"\"): %v", err)
			r.step("after one change to each", files...)
			r.note("-- log\n%s", logged.String())
			r.compare("xuid/mixed_" + mode)
		})
	}
}

// TestAdminFilesBrokenXUID pins the errors of entries of accounts the lists
// refuse to load, rather than drop.
func TestAdminFilesBrokenXUID(t *testing.T) {
	var b strings.Builder
	for _, tc := range []struct{ file, data string }{
		{"ops.json", `[{"xuid": "12a", "name": "Steve"}]`},
		{"ops.json", `[5]`},
		{"banned-players.json", `{"players": [{"xuid": "x", "name": "Steve"}]}`},
	} {
		dir := t.TempDir()
		r := &adminRecord{t: t, dir: dir}
		r.write(tc.file, tc.data)
		uc := DefaultConfig()
		uc.World.SaveData, uc.Players.SaveData = false, false
		uc.Whitelist.File = filepath.Join(dir, "white-list.txt")
		_, err := uc.Config(slog.New(slog.DiscardHandler))
		fmt.Fprintf(&b, "%s %q: %v\n", tc.file, tc.data, err)
	}
	compareAdminGolden(t, "xuid/broken", b.String())
}

// TestAdminFilesAdmissionXUID pins the verdict and the message of the
// Allower of a server keyed by XUID, for each kind of player, with the
// whitelist on and off.
func TestAdminFilesAdmissionXUID(t *testing.T) {
	r := newAdminRecord(t)
	r.write("white-list.txt", "xuid:"+steveXUID+" Steve\r\n")
	r.write("ops.json", `[{"xuid": "`+alexXUID+`", "name": "Alex"}, {"xuid": "2535412345678911", "name": "BannedOp"}]`)
	r.write("banned-players.json", `{"players": [{"xuid": "2535412345678910", "name": "Griefer", "reason": "griefing"}, {"xuid": "2535412345678911", "name": "BannedOp"}], "ips": [{"ip": "192.0.2.8", "reason": "abuse"}]}`)
	home := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 19132}
	banned := &net.UDPAddr{IP: net.IPv4(192, 0, 2, 8), Port: 19132}
	for _, enabled := range []bool{true, false} {
		srv, _ := xuidConfig(t, r.path("white-list.txt"), enabled)
		state := "off"
		if enabled {
			state = "on"
		}
		for _, tc := range []struct {
			desc string
			addr net.Addr
			id   client.Identity
		}{
			{"banned account", home, account("Griefer", "2535412345678910")},
			{"banned account, renamed", home, account("Renamed", "2535412345678910")},
			{"another account named as a banned one", home, account("Griefer", "2535412345678920")},
			{"banned address", banned, account("Stranger", "2535412345678921")},
			{"banned operator", home, account("BannedOp", "2535412345678911")},
			{"operator off the whitelist", home, account("Alex", alexXUID)},
			{"operator, renamed", home, account("Alexander", alexXUID)},
			{"another account named as an operator", home, account("Alex", "2535412345678922")},
			{"member", home, account("Steve", steveXUID)},
			{"member, renamed", home, account("Stephen", steveXUID)},
			{"another account named as a member", home, account("Steve", "2535412345678923")},
			{"stranger", home, account("Stranger", "2535412345678924")},
			{"self-named XUID of an operator", home, client.Identity{Name: "Alex", XUID: alexXUID}},
			{"self-named XUID of a member", home, client.Identity{Name: "Steve", XUID: steveXUID}},
			{"no XUID", home, client.Identity{Name: "Steve", Authenticated: true}},
			{"XUID that is not one", home, client.Identity{Name: "Steve", XUID: "steve", Authenticated: true}},
		} {
			msg, ok := srv.conf.Allower.Allow(tc.addr, tc.id, client.Profile{})
			verdict := "allowed"
			if !ok {
				verdict = "refused"
			}
			r.note("whitelist %-3s | %-37s | %q xuid %q authenticated %-5t | %s %q", state, tc.desc, tc.id.Name, tc.id.XUID, tc.id.Authenticated, verdict, msg)
		}
	}
	r.compare("xuid/allow")
}

// TestPendingNameBindsAtFirstLogin checks that an operator held by name
// becomes the first account that joins with the name, in any case, and
// only that account, and that the file is written with it.
func TestPendingNameBindsAtFirstLogin(t *testing.T) {
	dir := t.TempDir()
	srv, logged := xuidConfig(t, filepath.Join(dir, "white-list.txt"), true)
	if _, err := srv.Op("Steve"); err != nil {
		t.Fatal(err)
	}
	first, second := account("STEVE", steveXUID), account("Steve", otherXUID)
	if srv.operatorOf(first) {
		t.Fatal("an account is the operator held by name before it joined")
	}
	if _, ok := srv.conf.Allower.Allow(nil, first, client.Profile{}); !ok {
		t.Fatal("the first account that joins as Steve is refused")
	}
	if !srv.operatorOf(first) || srv.operatorOf(second) {
		t.Error("the operator held by name is not the first account that joined with it alone")
	}
	if _, ok := srv.conf.Allower.Allow(nil, second, client.Profile{}); ok {
		t.Error("a second account that joins as Steve passes the whitelist")
	}
	reloaded, err := loadOperators(filepath.Join(dir, "ops.json"))
	if err != nil {
		t.Fatal(err)
	}
	k, _ := identity.OfXUID(steveXUID)
	if !reloaded.hasKey(k) {
		t.Error("ops.json does not hold the bound account")
	}
	if want := `level=INFO msg="Bound the operator entry \"steve\" to xuid:` + steveXUID + ` (STEVE)."`; !strings.Contains(logged.String(), want) {
		t.Errorf("logged\n%s\nwant %s", logged.String(), want)
	}
}

// TestUnauthenticatedIdentityIsRefusedInXUIDMode checks that a server
// keyed by XUID refuses an identity Xbox Live did not vouch for, or with no
// XUID, both in its Allower and as the identity joins, whatever let it in.
func TestUnauthenticatedIdentityIsRefusedInXUIDMode(t *testing.T) {
	srv, _ := xuidConfig(t, filepath.Join(t.TempDir(), "white-list.txt"), false)
	for _, id := range []client.Identity{{Name: "Steve", XUID: steveXUID}, {Name: "Steve", Authenticated: true}} {
		if msg, ok := srv.conf.Allower.Allow(nil, id, client.Profile{}); ok || msg != "This server needs an Xbox Live account." {
			t.Errorf("Allow(%+v) = %t %q", id, ok, msg)
		}
	}

	jt := newJoinTest(t, player.NopProvider{}, Config{IdentityKey: identity.XUID})
	jt.start()
	self := joinLogin("Steve", steveXUID)
	jt.l.Send(self)
	awaitRejected(t, self, "This server needs an Xbox Live account.")
	if len(self.Games()) != 0 {
		t.Error("the game of a client nobody vouched for was started")
	}
	signed := joinLogin("Steve", steveXUID)
	signed.L.Identity.Authenticated = true
	jt.join(t, signed)
}

// TestSelfNamedXUIDNeverMatches checks that on a server keyed by name, as
// on one keyed by XUID, an XUID a client claims matches no entry of an
// account: on one keyed by name, entries match by name alone.
func TestSelfNamedXUIDNeverMatches(t *testing.T) {
	r := newAdminRecord(t)
	r.write("white-list.txt", "xuid:"+steveXUID+" Steve\r\n")
	r.write("ops.json", `[{"xuid": "`+steveXUID+`", "name": "Steve"}]`)
	byName := adminConfig(t, r.path("white-list.txt"), true)
	claim := client.Identity{Name: "Mallory", XUID: steveXUID, UUID: uuid.New()}
	if _, ok := byName.conf.Allower.Allow(nil, claim, client.Profile{}); ok || byName.operatorOf(claim) {
		t.Error("a client that claims the XUID of an operator passes on a server keyed by name")
	}
	byXUID, _ := xuidConfig(t, r.path("white-list.txt"), true)
	if _, ok := byXUID.conf.Allower.Allow(nil, claim, client.Profile{}); ok || byXUID.operatorOf(claim) {
		t.Error("a client that claims the XUID of an operator passes on a server keyed by XUID")
	}
}

// TestRenamedPlayerKeepsOperator checks that on a server keyed by XUID an
// account that renames itself stays an operator and on the whitelist, and
// that another account that takes the old name is neither. It is the other
// side of TestRenamedPlayerLosesOperator, which holds for a server keyed
// by name.
func TestRenamedPlayerKeepsOperator(t *testing.T) {
	srv, _ := xuidConfig(t, filepath.Join(t.TempDir(), "white-list.txt"), true)
	if _, err := srv.Op("Steve"); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.WhitelistAdd("Steve"); err != nil {
		t.Fatal(err)
	}
	if _, ok := srv.conf.Allower.Allow(nil, account("Steve", steveXUID), client.Profile{}); !ok {
		t.Fatal("Steve is refused")
	}
	if _, ok := srv.conf.Allower.Allow(nil, account("Bob", steveXUID), client.Profile{}); !ok || !srv.operatorOf(account("Bob", steveXUID)) {
		t.Error("the account of Steve renamed to Bob is not an operator")
	}
	newcomer := account("Steve", otherXUID)
	if _, ok := srv.conf.Allower.Allow(nil, newcomer, client.Profile{}); ok || srv.operatorOf(newcomer) {
		t.Error("another account that took the name Steve is his operator or on the whitelist")
	}
}
