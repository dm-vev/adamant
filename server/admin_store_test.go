package server

import (
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/dm-vev/adamant/net/bedrock/login"
)

func TestOperatorChangesPermissionAndPersist(t *testing.T) {
	ops, err := loadOperators(filepath.Join(t.TempDir(), "ops.json"))
	if err != nil {
		t.Fatal(err)
	}
	srv := &Server{operators: ops}
	if srv.IsOperator("Alice") {
		t.Fatal("new player unexpectedly has operator permission")
	}
	if added, err := srv.Op("Alice"); err != nil || !added {
		t.Fatalf("Op() = %t, %v", added, err)
	}
	if !srv.IsOperator("alice") {
		t.Fatal("op did not grant permission")
	}
	loaded, err := loadOperators(ops.filePath)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.isOperator("ALICE") {
		t.Fatal("operator status was not persisted")
	}
	if removed, err := srv.Deop("Alice"); err != nil || !removed {
		t.Fatalf("Deop() = %t, %v", removed, err)
	}
	if srv.IsOperator("Alice") {
		t.Fatal("deop did not revoke permission")
	}
}

func TestBansRejectPlayerAndIPAtLogin(t *testing.T) {
	bans, err := loadBans(filepath.Join(t.TempDir(), "bans.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bans.addPlayer("Alice", "griefing"); err != nil {
		t.Fatal(err)
	}
	if removed, err := bans.removePlayer("alice"); err != nil || !removed {
		t.Fatalf("remove player ban = %t, %v", removed, err)
	}
	if added, err := bans.addPlayer("Alice", "again"); err != nil || !added {
		t.Fatalf("re-add player ban = %t, %v", added, err)
	}
	if entries := bans.entries(false); len(entries) != 1 || entries[0] != "alice" {
		t.Fatalf("player bans after pardon/re-ban = %v, want [alice]", entries)
	}
	if _, err := bans.addIP("192.0.2.8", "abuse"); err != nil {
		t.Fatal(err)
	}
	allow := adminAllower{base: allower{}, bans: bans}
	for _, tc := range []struct {
		name string
		addr net.Addr
	}{
		{name: "ALICE", addr: &net.TCPAddr{IP: net.IPv4(192, 0, 2, 1)}},
		{name: "Bob", addr: &net.TCPAddr{IP: net.IPv4(192, 0, 2, 8)}},
	} {
		message, ok := allow.Allow(tc.addr, login.Identity{DisplayName: tc.name}, login.ClientData{})
		if ok || message == "" {
			t.Errorf("Allow(%s, %v) = %q, %t; want ban rejection", tc.name, tc.addr, message, ok)
		}
	}
	if message, ok := allow.Allow(&net.TCPAddr{IP: net.IPv4(192, 0, 2, 9)}, login.Identity{DisplayName: "Bob"}, login.ClientData{}); !ok || message != "" {
		t.Fatalf("unbanned login = %q, %t; want allowed", message, ok)
	}
}

func TestIdleTimeoutCanBeChangedAndDisabled(t *testing.T) {
	srv := &Server{}
	srv.SetIdleTimeout(5)
	if got := srv.IdleTimeout(); got != 5 {
		t.Fatalf("IdleTimeout() = %d, want 5", got)
	}
	srv.SetIdleTimeout(0)
	if got := srv.IdleTimeout(); got != 0 {
		t.Fatalf("IdleTimeout() = %d, want disabled", got)
	}
}
func TestIdleTimeoutExpiryBoundary(t *testing.T) {
	now := time.Unix(1000, 0)
	timeout := 5 * time.Minute
	if idleTimeoutExpired(now.Add(-timeout+time.Second), now, timeout) {
		t.Fatal("recently active player considered idle")
	}
	if !idleTimeoutExpired(now.Add(-timeout), now, timeout) {
		t.Fatal("player at timeout boundary not considered idle")
	}
	if idleTimeoutExpired(time.Time{}, now, timeout) {
		t.Fatal("missing activity timestamp considered idle")
	}
}
