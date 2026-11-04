package server

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/identity"
	"github.com/df-mc/dragonfly/server/player"
)

// TestRenameUpdatesTheLists checks that, on a server keyed by XUID, the
// entries of an account take the name it joins with, in each list and its
// file, with an Info line when the name changed; an entry of an account
// named by XUID alone takes its first name quietly; the operator status
// stays with the account.
func TestRenameUpdatesTheLists(t *testing.T) {
	dir := t.TempDir()
	r := &adminRecord{t: t, dir: dir}
	r.write("white-list.txt", "xuid:"+steveXUID+" Steve\r\nxuid:"+alexXUID+"\r\n")
	r.write("ops.json", `[{"xuid": "`+steveXUID+`", "name": "Steve"}]`)
	r.write("banned-players.json", `{"players": [{"xuid": "`+otherXUID+`", "name": "Griefer", "reason": "griefing"}], "ips": []}`)
	r.write("known-players.json", `[{"xuid": "`+steveXUID+`", "name": "Steve", "lastSeen": "2026-10-01T00:00:00Z"}]`)
	uc := DefaultConfig()
	uc.World.SaveData, uc.Players.SaveData = false, false
	uc.Whitelist.File, uc.Whitelist.Enabled = filepath.Join(dir, "white-list.txt"), true
	uc.Server.IdentityKey = "xuid"
	var logged bytes.Buffer
	conf, err := uc.Config(slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{ReplaceAttr: noTime})))
	if err != nil {
		t.Fatal(err)
	}
	jt := newJoinTest(t, player.NopProvider{}, conf)
	jt.srv.conf.Log = slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{ReplaceAttr: noTime}))
	jt.start()
	for _, j := range []struct{ name, xuid string }{{"Bob", steveXUID}, {"Alex", alexXUID}} {
		l := joinLogin(j.name, j.xuid)
		l.L.Identity.Authenticated = true
		jt.join(t, l)
	}
	wl, _ := os.ReadFile(filepath.Join(dir, "white-list.txt"))
	if want := "xuid:" + steveXUID + " Bob\r\nxuid:" + alexXUID + " Alex\r\n"; string(wl) != want {
		t.Errorf("white-list.txt:\n%q\nwant:\n%q", wl, want)
	}
	ops, _ := os.ReadFile(filepath.Join(dir, "ops.json"))
	if !strings.Contains(string(ops), `"name": "Bob"`) {
		t.Errorf("ops.json:\n%s", ops)
	}
	if entries, _ := jt.srv.OperatorEntries(); len(entries) != 1 || entries[0] != "Bob (xuid:"+steveXUID+")" {
		t.Errorf("OperatorEntries() = %q", entries)
	}
	if !jt.srv.IsOperator("bob") || jt.srv.IsOperator("steve") {
		t.Error("the operator status did not follow the account to its new name")
	}
	if want := `level=INFO msg="xuid:` + steveXUID + ` renamed: Steve → Bob."`; !strings.Contains(logged.String(), want) {
		t.Errorf("logged\n%s\nwant %s", logged.String(), want)
	}
	if strings.Contains(logged.String(), "xuid:"+alexXUID+" renamed") {
		t.Errorf("the first name of an account was logged as a rename:\n%s", logged.String())
	}
	if _, ok := jt.srv.known.account(steveXUID); !ok {
		t.Fatal("the account of Bob is not known")
	}
	if p, _ := jt.srv.known.named("Bob"); p.XUID != steveXUID {
		t.Errorf("bob is %+v", p)
	}
	k, _ := identity.OfXUID(otherXUID)
	var ban entry
	jt.srv.bans.view(func(e *entries) { ban, _ = e.get(k) })
	if ban.name != "Griefer" {
		t.Errorf("the ban of an account that did not join was renamed %q", ban.name)
	}
}
