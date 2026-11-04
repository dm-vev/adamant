package server

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/identity"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/google/uuid"
)

// TestKnownPlayersLatestSightingWins checks that a name is the account that
// last joined with it, in any case, that an account that renames leaves its
// old name to whoever took it, and that the file keeps all of it.
func TestKnownPlayersLatestSightingWins(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known-players.json")
	k, existed, err := loadKnownPlayers(path, nil)
	if err != nil || existed {
		t.Fatalf("load of no file: %t, %v", existed, err)
	}
	clock := time.Date(2026, 11, 4, 1, 0, 0, 0, time.UTC)
	k.now = func() time.Time { clock = clock.Add(time.Minute); return clock }
	a, b := uuid.New(), uuid.New()
	if prev := k.seen(steveXUID, "Steve", a); prev != "" {
		t.Errorf("first sighting: previous name %q", prev)
	}
	k.seen(otherXUID, "STEVE", b)
	if p, ok := k.named("steve"); !ok || p.XUID != otherXUID {
		t.Errorf("steve is %+v, want the account that joined with it last", p)
	}
	if prev := k.seen(steveXUID, "Bob", a); prev != "Steve" {
		t.Errorf("rename: previous name %q, want Steve", prev)
	}
	if p, ok := k.named("BOB"); !ok || p.XUID != steveXUID || p.UUID != a {
		t.Errorf("bob is %+v", p)
	}
	if p, _ := k.named("steve"); p.XUID != otherXUID {
		t.Errorf("steve after the rename of the other is %+v", p)
	}
	if err := k.close(); err != nil {
		t.Fatal(err)
	}
	again, existed, err := loadKnownPlayers(path, nil)
	if err != nil || !existed {
		t.Fatalf("reload: %t, %v", existed, err)
	}
	if p, ok := again.named("steve"); !ok || p.XUID != otherXUID || p.Name != "STEVE" || p.UUID != b {
		t.Errorf("steve after a reload is %+v", p)
	}
	if p, ok := again.account(steveXUID); !ok || p.Name != "Bob" || !p.LastSeen.Equal(time.Date(2026, 11, 4, 1, 3, 0, 0, time.UTC)) {
		t.Errorf("the account of Bob after a reload is %+v", p)
	}
}

// TestKnownPlayersOfOneNameInTheFile checks that two accounts the file
// gives one name to leave it to the one seen last, in either order.
func TestKnownPlayersOfOneNameInTheFile(t *testing.T) {
	for _, data := range []string{
		`[{"xuid": "` + steveXUID + `", "name": "Steve", "lastSeen": "2026-10-01T00:00:00Z"}, {"xuid": "` + otherXUID + `", "name": "steve", "lastSeen": "2026-11-01T00:00:00Z"}]`,
		`[{"xuid": "` + otherXUID + `", "name": "steve", "lastSeen": "2026-11-01T00:00:00Z"}, {"xuid": "` + steveXUID + `", "name": "Steve", "lastSeen": "2026-10-01T00:00:00Z"}]`,
	} {
		path := filepath.Join(t.TempDir(), "known-players.json")
		if err := os.WriteFile(path, []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
		k, _, err := loadKnownPlayers(path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if p, _ := k.named("Steve"); p.XUID != otherXUID {
			t.Errorf("%s: steve is %+v", data, p)
		}
	}
}

// TestKnownPlayersWriteAfterTheDebounce checks that the file is written once
// a sighting is a debounce old, not before, and at close even with nothing
// seen.
func TestKnownPlayersWriteAfterTheDebounce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "known-players.json")
	k, _, _ := loadKnownPlayers(path, nil)
	k.debounce = 50 * time.Millisecond
	k.seen(steveXUID, "Steve", uuid.New())
	if _, err := os.Stat(path); !errNotExist(err) {
		t.Fatalf("the file was written before the debounce: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the file was not written after the debounce")
		}
		time.Sleep(10 * time.Millisecond)
	}
	empty := filepath.Join(dir, "empty.json")
	k2, _, _ := loadKnownPlayers(empty, nil)
	if err := k2.close(); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(empty); err != nil || string(data) != "[]\n" {
		t.Errorf("the file written at close of none: %q, %v", data, err)
	}
}

// TestJoinRemembersTheAccount checks that a server keyed by XUID remembers
// the account of each player that joins, and writes it as it closes.
func TestJoinRemembersTheAccount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known-players.json")
	jt := newJoinTest(t, player.NopProvider{}, Config{IdentityKey: identity.XUID})
	var err error
	if jt.srv.known, _, err = loadKnownPlayers(path, nil); err != nil {
		t.Fatal(err)
	}
	jt.start()
	j := joinLogin("Steve", steveXUID)
	j.L.Identity.Authenticated = true
	jt.join(t, j)
	if p, ok := jt.srv.known.named("steve"); !ok || p.XUID != steveXUID || p.UUID != j.L.Identity.UUID {
		t.Errorf("steve is %+v", p)
	}
	if err := jt.srv.Close(); err != nil {
		t.Fatal(err)
	}
	k, existed, err := loadKnownPlayers(path, nil)
	if err != nil || !existed {
		t.Fatalf("the file after the close: %t, %v", existed, err)
	}
	if p, ok := k.named("Steve"); !ok || p.XUID != steveXUID {
		t.Errorf("steve in the file is %+v", p)
	}
}
