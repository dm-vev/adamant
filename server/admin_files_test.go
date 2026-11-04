package server

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/client"
	"github.com/google/uuid"
)

// The tests of this file pin the files of the whitelist, the operators and
// the bans to their bytes, and the admission the three lists make together,
// as UserConfig.Config composes them: each step and the file after it are
// compared with a golden of testdata/admin. Run with -update-admin to
// record them again.

var updateAdmin = flag.Bool("update-admin", false, "record the files of the admin lists in testdata/admin again")

// adminRecord is the record of a sequence of steps on the admin lists.
type adminRecord struct {
	t   *testing.T
	b   strings.Builder
	dir string
}

// newAdminRecord returns a record of steps on files in a directory of the
// test.
func newAdminRecord(t *testing.T) *adminRecord {
	t.Helper()
	return &adminRecord{t: t, dir: t.TempDir()}
}

// path returns the path of the file of the name passed in the directory
// of the record.
func (r *adminRecord) path(name string) string { return filepath.Join(r.dir, name) }

// write writes a file of the record, as an admin or an older server left
// it.
func (r *adminRecord) write(name, data string) {
	r.t.Helper()
	if err := os.WriteFile(r.path(name), []byte(data), 0644); err != nil {
		r.t.Fatal(err)
	}
}

// step records a step, what it returned, and the bytes of the files named
// after it, in the order named.
func (r *adminRecord) step(desc string, files ...string) {
	r.t.Helper()
	fmt.Fprintf(&r.b, "== %s\n", desc)
	for _, name := range files {
		data, err := os.ReadFile(r.path(name))
		switch {
		case errors.Is(err, fs.ErrNotExist):
			fmt.Fprintf(&r.b, "-- %s: none\n", name)
		case err != nil:
			r.t.Fatal(err)
		default:
			fmt.Fprintf(&r.b, "-- %s (%d bytes)\n%s", name, len(data), quoteLines(data))
		}
	}
}

// note records a line of its own.
func (r *adminRecord) note(format string, args ...any) {
	fmt.Fprintf(&r.b, format+"\n", args...)
}

// quoteLines writes data a line at a time, each line quoted, so that
// carriage returns, spaces at the end and a missing last newline show.
func quoteLines(data []byte) string {
	var b strings.Builder
	rest := string(data)
	for rest != "" {
		line, after, found := strings.Cut(rest, "\n")
		if found {
			line += "\n"
		}
		fmt.Fprintf(&b, "   %q\n", line)
		rest = after
	}
	return b.String()
}

// compare compares the record with testdata/admin/<name>.golden, or
// writes it there with -update-admin.
func (r *adminRecord) compare(name string) {
	r.t.Helper()
	compareAdminGolden(r.t, name, r.b.String())
}

func compareAdminGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "admin", name+".golden")
	if *updateAdmin {
		if err := os.MkdirAll(filepath.Dir(path), 0777); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v: run with -update-admin to record it", err)
	}
	if string(want) != got {
		t.Errorf("%s differs (run with -update-admin if the change is meant):\n%s", path, adminDiff(string(want), got))
	}
}

// adminDiff returns the lines of want and got that differ, each with its
// line number.
func adminDiff(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	var b strings.Builder
	for i := 0; i < max(len(w), len(g)); i++ {
		var wl, gl string
		if i < len(w) {
			wl = w[i]
		}
		if i < len(g) {
			gl = g[i]
		}
		if wl != gl {
			fmt.Fprintf(&b, "%d:\n  want %s\n  got  %s\n", i+1, wl, gl)
		}
	}
	return b.String()
}

// adminServer returns a Server that holds the admin lists of conf as
// Config.New takes them from its Allower, without its worlds.
func adminServer(t *testing.T, conf Config) *Server {
	t.Helper()
	srv := &Server{conf: conf}
	if wl, ok := conf.Allower.(*Whitelist); ok {
		srv.whitelist = wl
	} else if holder, ok := conf.Allower.(interface{ underlyingWhitelist() *Whitelist }); ok {
		srv.whitelist = holder.underlyingWhitelist()
	}
	if access, ok := conf.Allower.(interface {
		adminAccess() (*operatorsList, *banList)
	}); ok {
		srv.operators, srv.bans = access.adminAccess()
	}
	if srv.whitelist == nil || srv.operators == nil || srv.bans == nil {
		t.Fatal("the Allower of the Config does not hold the admin lists")
	}
	return srv
}

// adminConfig returns the Config UserConfig.Config makes of a whitelist at
// the path passed, with no world and no player data, and the Server of its
// admin lists.
func adminConfig(t *testing.T, whitelist string, enabled bool) *Server {
	t.Helper()
	uc := DefaultConfig()
	uc.World.SaveData, uc.Players.SaveData = false, false
	uc.Whitelist.File, uc.Whitelist.Enabled = whitelist, enabled
	conf, err := uc.Config(slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	return adminServer(t, conf)
}

// TestAdminFilesWhitelist pins what WhitelistAdd and WhitelistRemove write
// to a whitelist in each of its formats.
func TestAdminFilesWhitelist(t *testing.T) {
	for _, name := range []string{"white-list.txt", "white-list.toml"} {
		t.Run(name, func(t *testing.T) {
			r := newAdminRecord(t)
			srv := adminConfig(t, r.path(name), true)
			r.step("loaded with no file", name)
			for _, s := range []struct {
				add  bool
				name string
			}{
				{true, "Steve"}, {true, "ALEX"}, {true, "steve"}, {true, "  Alex Two  "}, {true, ""}, {true, "   "},
				{false, "STEVE"}, {false, "nobody"}, {false, ""}, {true, "Steve"},
			} {
				op, f := "WhitelistAdd", srv.WhitelistAdd
				if !s.add {
					op, f = "WhitelistRemove", srv.WhitelistRemove
				}
				ok, err := f(s.name)
				r.step(fmt.Sprintf("%s(%q) = %t, %v", op, s.name, ok, err), name)
			}
			entries, err := srv.WhitelistEntries()
			r.note("WhitelistEntries() = %q, %v", entries, err)
			r.compare(strings.ReplaceAll(name, ".", "_"))
		})
	}
}

// TestAdminFilesOperators pins what Op and Deop write to ops.json, and
// whom IsOperator then holds an operator.
func TestAdminFilesOperators(t *testing.T) {
	r := newAdminRecord(t)
	srv := adminConfig(t, r.path("white-list.txt"), false)
	r.step("loaded with no file", "ops.json")
	for _, s := range []struct {
		op   bool
		name string
	}{
		{true, "Steve"}, {true, "ALEX"}, {true, "steve"}, {true, " Alex Two "}, {true, ""},
		{false, "STEVE"}, {false, "nobody"}, {false, ""}, {true, "Steve"},
	} {
		op, f := "Op", srv.Op
		if !s.op {
			op, f = "Deop", srv.Deop
		}
		ok, err := f(s.name)
		r.step(fmt.Sprintf("%s(%q) = %t, %v", op, s.name, ok, err), "ops.json")
		r.note("IsOperator: steve %t, ALEX %t, alex two %t, \"\" %t",
			srv.IsOperator("steve"), srv.IsOperator("ALEX"), srv.IsOperator("alex two"), srv.IsOperator(""))
	}
	entries, err := srv.OperatorEntries()
	r.note("OperatorEntries() = %q, %v", entries, err)
	r.compare("ops_json")
}

// TestAdminFilesBans pins what BanPlayer, PardonPlayer, BanIP and PardonIP
// write to banned-players.json, and what Banned then finds.
func TestAdminFilesBans(t *testing.T) {
	r := newAdminRecord(t)
	srv := adminConfig(t, r.path("white-list.txt"), false)
	const file = "banned-players.json"
	r.step("loaded with no file", file)
	for _, s := range []struct {
		desc string
		f    func() (bool, error)
	}{
		{`BanPlayer("Griefer", "griefing")`, func() (bool, error) { return srv.BanPlayer("Griefer", "griefing") }},
		{`BanPlayer("GRIEFER", "again")`, func() (bool, error) { return srv.BanPlayer("GRIEFER", "again") }},
		{`BanPlayer(" Alex ", "")`, func() (bool, error) { return srv.BanPlayer(" Alex ", "") }},
		{`BanPlayer("", "nobody")`, func() (bool, error) { return srv.BanPlayer("", "nobody") }},
		{`BanIP("192.0.2.8:19132", "abuse")`, func() (bool, error) { return srv.BanIP("192.0.2.8:19132", "abuse") }},
		{`BanIP("192.0.2.8", "again")`, func() (bool, error) { return srv.BanIP("192.0.2.8", "again") }},
		{`BanIP("[2001:db8::1]:19132", "")`, func() (bool, error) { return srv.BanIP("[2001:db8::1]:19132", "") }},
		{`BanIP("not an address", "")`, func() (bool, error) { return srv.BanIP("not an address", "") }},
		{`PardonPlayer("griefer")`, func() (bool, error) { return srv.PardonPlayer("griefer") }},
		{`PardonPlayer("nobody")`, func() (bool, error) { return srv.PardonPlayer("nobody") }},
		{`PardonIP("192.0.2.8")`, func() (bool, error) { return srv.PardonIP("192.0.2.8") }},
		{`BanPlayer("Griefer", "")`, func() (bool, error) { return srv.BanPlayer("Griefer", "") }},
	} {
		ok, err := s.f()
		r.step(fmt.Sprintf("%s = %t, %v", s.desc, ok, err), file)
	}
	players, err := srv.BanEntries(false)
	r.note("BanEntries(false) = %q, %v", players, err)
	ips, err := srv.BanEntries(true)
	r.note("BanEntries(true) = %q, %v", ips, err)
	for _, q := range []struct{ addr, name string }{
		{"127.0.0.1:1", "GRIEFER"}, {"127.0.0.1:1", "alex"}, {"[2001:db8::1]:5", "Steve"}, {"192.0.2.8:1", "Steve"}, {"", ""},
	} {
		reason, ok := srv.Banned(q.addr, q.name)
		r.note("Banned(%q, %q) = %q, %t", q.addr, q.name, reason, ok)
	}
	r.compare("banned_players_json")
}

// TestAdminFilesLegacy pins how the lists read files that an admin or an
// older server wrote, and what they write back after one change.
func TestAdminFilesLegacy(t *testing.T) {
	r := newAdminRecord(t)
	r.write("white-list.txt", "Steve\r\n\r\n  alex  \nSTEVE\n\n Alex Two\r\n   \r\nlast")
	r.write("white-list.toml", "players = [\"Steve\", \"\", \"  \", \" ALEX \", \"steve\", \"Alex Two\"]\n")
	r.write("ops.json", "[\"Steve\", \" ALEX \", \"\", \"steve\", \"Alex Two\"]")
	r.write("banned-players.json", `{"players": [{"name": "Griefer", "reason": "first"}, {"name": " GRIEFER ", "reason": "second"}, {"name": ""}, {"name": "Alex", "ip": "192.0.2.1"}],
 "ips": [{"ip": "192.0.2.8:19132", "reason": "abuse"}, {"ip": "bad"}, {"ip": "192.0.2.8"}, {"name": "Steve", "ip": "[2001:db8::1]:1"}]}`)
	files := []string{"white-list.txt", "white-list.toml", "ops.json", "banned-players.json"}
	r.step("written by hand", files...)

	srv := adminConfig(t, r.path("white-list.txt"), true)
	toml, err := LoadWhitelist(r.path("white-list.toml"))
	if err != nil {
		t.Fatal(err)
	}
	r.step("loaded: nothing is written back", files...)
	entries, err := srv.WhitelistEntries()
	r.note("WhitelistEntries() = %q, %v", entries, err)
	r.note("toml Players() = %q", toml.Players())
	ops, err := srv.OperatorEntries()
	r.note("OperatorEntries() = %q, %v", ops, err)
	players, err := srv.BanEntries(false)
	r.note("BanEntries(false) = %q, %v", players, err)
	ips, err := srv.BanEntries(true)
	r.note("BanEntries(true) = %q, %v", ips, err)
	reason, banned := srv.Banned("", "griefer")
	r.note("Banned(\"\", \"griefer\") = %q, %t", reason, banned)

	_, err = srv.WhitelistAdd("New")
	r.note("WhitelistAdd(\"New\"): %v", err)
	_, err = toml.Add("New")
	r.note("toml Add(\"New\"): %v", err)
	_, err = srv.Op("New")
	r.note("Op(\"New\"): %v", err)
	_, err = srv.BanPlayer("New", "")
	r.note("BanPlayer(\"New\", \"\"): %v", err)
	r.step("after one change to each", files...)
	r.compare("legacy")
}

// TestAdminFilesBrokenFiles pins the errors of files the lists refuse to
// load.
func TestAdminFilesBrokenFiles(t *testing.T) {
	var b strings.Builder
	for _, tc := range []struct{ file, data string }{
		{"ops.json", "{\"steve\": true}"},
		{"ops.json", "not json"},
		{"banned-players.json", "[\"steve\"]"},
		{"white-list.toml", "players = 5\n"},
	} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, tc.file), []byte(tc.data), 0644); err != nil {
			t.Fatal(err)
		}
		uc := DefaultConfig()
		uc.World.SaveData, uc.Players.SaveData = false, false
		uc.Whitelist.File = filepath.Join(dir, "white-list.txt")
		if tc.file == "white-list.toml" {
			uc.Whitelist.File = filepath.Join(dir, tc.file)
		}
		_, err := uc.Config(slog.New(slog.DiscardHandler))
		msg := "<nil>"
		if err != nil {
			msg = strings.ReplaceAll(err.Error(), dir+string(filepath.Separator), "")
		}
		fmt.Fprintf(&b, "%s %q: %s\n", tc.file, tc.data, msg)
	}
	compareAdminGolden(t, "broken", b.String())
}

// TestAdminFilesAdmission pins the verdict and the message of the Allower
// UserConfig.Config composes of the three lists, for each kind of player,
// with the whitelist on and off.
func TestAdminFilesAdmission(t *testing.T) {
	r := newAdminRecord(t)
	r.write("white-list.txt", "member\r\n")
	r.write("ops.json", "[\n  \"op\",\n  \"bannedop\"\n]\n")
	r.write("banned-players.json", `{"players": [{"name": "griefer", "reason": "griefing"}, {"name": "bannedop"}], "ips": [{"ip": "192.0.2.8", "reason": "abuse"}]}`)
	home := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 19132}
	banned := &net.UDPAddr{IP: net.IPv4(192, 0, 2, 8), Port: 19132}
	for _, enabled := range []bool{true, false} {
		srv := adminConfig(t, r.path("white-list.txt"), enabled)
		state := "off"
		if enabled {
			state = "on"
		}
		for _, tc := range []struct {
			desc string
			addr net.Addr
			id   client.Identity
		}{
			{"banned by name", home, client.Identity{Name: "Griefer", XUID: "2535400000000001"}},
			{"banned by name, other case", home, client.Identity{Name: "GRIEFER"}},
			{"banned by address", banned, client.Identity{Name: "Stranger"}},
			{"banned by address, a member", banned, client.Identity{Name: "Member"}},
			{"banned operator", home, client.Identity{Name: "BannedOp"}},
			{"operator off the whitelist", home, client.Identity{Name: "Op", XUID: "2535400000000002"}},
			{"operator, other case", home, client.Identity{Name: "OP"}},
			{"operator from a banned address", banned, client.Identity{Name: "Op"}},
			{"member", home, client.Identity{Name: "Member"}},
			{"member, other case", home, client.Identity{Name: "MEMBER"}},
			{"stranger", home, client.Identity{Name: "Stranger"}},
			{"stranger with no address", nil, client.Identity{Name: "Stranger"}},
			{"stranger with the XUID of nobody", home, client.Identity{Name: "Stranger", XUID: "2535400000000003"}},
			{"empty name", home, client.Identity{Name: ""}},
			{"blank name", home, client.Identity{Name: "   "}},
		} {
			tc.id.UUID = uuid.NewMD5(uuid.Nil, []byte(tc.id.Name))
			msg, ok := srv.conf.Allower.Allow(tc.addr, tc.id, client.Profile{})
			verdict := "allowed"
			if !ok {
				verdict = "refused"
			}
			r.note("whitelist %-3s | %-33s | %q xuid %q | %s %q", state, tc.desc, tc.id.Name, tc.id.XUID, verdict, msg)
		}
	}
	r.compare("allow")
}
