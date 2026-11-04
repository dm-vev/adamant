package server

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/audit"
	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/identity"
	"github.com/google/uuid"
)

// banClock is the clock of the bans of a test: it reads now, which the
// test moves.
type banClock struct{ now time.Time }

func (c *banClock) Now() time.Time { return c.now }

// banTestServer returns a server keyed by name, or by XUID if xuid, whose
// bans expire by the clock returned, and the names and addresses of the
// bans that expired, as they do.
func banTestServer(t *testing.T, xuid bool) (*Server, *banClock, *[]string) {
	t.Helper()
	dir := t.TempDir()
	uc := DefaultConfig()
	uc.World.SaveData, uc.Players.SaveData = false, false
	uc.Whitelist.File = filepath.Join(dir, "white-list.txt")
	if xuid {
		uc.Server.IdentityKey = "xuid"
	}
	conf, err := uc.Config(nil)
	if err != nil {
		t.Fatal(err)
	}
	srv := adminServer(t, conf)
	clock := &banClock{now: time.Date(2026, 11, 4, 14, 20, 31, 0, time.UTC)}
	srv.bans.now = clock.Now
	var expired []string
	srv.bans.expired = func(players []entry, ips []banEntry) {
		for _, en := range players {
			expired = append(expired, en.key.String())
		}
		for _, e := range ips {
			expired = append(expired, e.IP)
		}
	}
	return srv, clock, &expired
}

// TestBanFieldsAndExpiry checks the fields of a ban: Ban records when, by
// whom and until when, in RFC 3339; the player is refused with the reason
// and the line of the expiry; once it expired, the player joins, the ban
// is taken off the file and its expiry is told; BanPlayer, as before,
// writes no field.
func TestBanFieldsAndExpiry(t *testing.T) {
	srv, clock, expired := banTestServer(t, false)
	steve := audit.Actor{Kind: audit.KindPlayer, Name: "Steve", Key: "xuid:2535498765432109"}
	until := clock.now.Add(7 * 24 * time.Hour)
	if ok, err := srv.Ban("Griefer", BanOptions{Reason: "griefing", By: steve, Expires: until}); !ok || err != nil {
		t.Fatalf("Ban = %v, %v", ok, err)
	}
	if ok, err := srv.BanPlayer("Old", "spam"); !ok || err != nil {
		t.Fatalf("BanPlayer = %v, %v", ok, err)
	}
	if ok, err := srv.BanAddress("203.0.113.7", BanOptions{Reason: "alts"}); !ok || err != nil {
		t.Fatalf("BanAddress = %v, %v", ok, err)
	}
	const file = "{\n  \"players\": [\n    {\n      \"name\": \"griefer\",\n      \"reason\": \"griefing\",\n" +
		"      \"created\": \"2026-11-04T14:20:31Z\",\n      \"source\": \"Steve (xuid:2535498765432109)\",\n" +
		"      \"expires\": \"2026-11-11T14:20:31Z\"\n    },\n    {\n      \"name\": \"old\",\n      \"reason\": \"spam\"\n    }\n  ],\n" +
		"  \"ips\": [\n    {\n      \"ip\": \"203.0.113.7\",\n      \"reason\": \"alts\",\n      \"created\": \"2026-11-04T14:20:31Z\",\n" +
		"      \"source\": \"API\"\n    }\n  ]\n}\n"
	if got := string(readTestFile(t, srv.bans.path())); got != file {
		t.Errorf("banned-players.json:\n%s\nwant\n%s", got, file)
	}
	addr := &net.UDPAddr{IP: net.IPv4(198, 51, 100, 1), Port: 4242}
	want := "You are banned from this server. Reason: griefing\nYour ban will be removed on " + until.Local().Format("2006-01-02 at 15:04:05 MST")
	if msg, ok := srv.conf.Allower.Allow(addr, client.Identity{Name: "Griefer", UUID: uuid.New()}, client.Profile{}); ok || msg != want {
		t.Errorf("Griefer at login: %q, %v, want %q", msg, ok, want)
	}
	if msg, _ := srv.conf.Allower.Allow(addr, client.Identity{Name: "Old", UUID: uuid.New()}, client.Profile{}); msg != "You are banned from this server. Reason: spam" {
		t.Errorf("Old at login: %q", msg)
	}

	clock.now = until
	if msg, ok := srv.conf.Allower.Allow(addr, client.Identity{Name: "Griefer", UUID: uuid.New()}, client.Profile{}); !ok {
		t.Errorf("Griefer, the ban expired: refused with %q", msg)
	}
	if len(*expired) != 1 || (*expired)[0] != "griefer" {
		t.Errorf("expired %v, want griefer", *expired)
	}
	if _, banned := srv.Banned("", "Griefer"); banned {
		t.Error("the expired ban applies")
	}
	entries, _ := srv.BanEntries(false)
	if len(entries) != 1 || entries[0] != "old" {
		t.Errorf("bans after the expiry: %q", entries)
	}
}

// TestBansReadJavaFields checks that an entry pasted from Java's
// banned-players.json reads: its date format, and "forever".
func TestBansReadJavaFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "banned-players.json")
	java := `{"players":[{"name":"Notch","created":"2026-11-04 14:20:31 +0100","source":"Server","expires":"forever","reason":"Banned by an operator."}],"ips":[]}`
	if err := os.WriteFile(path, []byte(java), 0644); err != nil {
		t.Fatal(err)
	}
	b, err := loadBans(path)
	if err != nil {
		t.Fatal(err)
	}
	en, ok := b.players.get(mustNameKey(t, "notch"))
	if !ok || !en.ban.created.Equal(time.Date(2026, 11, 4, 13, 20, 31, 0, time.UTC)) || en.ban.source != "Server" || !en.ban.expires.IsZero() {
		t.Errorf("the ban of Java: %+v, %v", en, ok)
	}
	if err := os.WriteFile(path, []byte(`{"players":[{"name":"x","created":"yesterday"}],"ips":[]}`), 0644); err == nil {
		if _, err := loadBans(path); err == nil {
			t.Error("a ban of a time of no format reads")
		}
	}
}

// TestPendingTempBanKeepsExpiry checks that a temporary ban of a name no
// account is known by, on a server keyed by XUID, binds to the first
// account that joins with it and keeps its expiry, its source and its
// time, counted from when it was made.
func TestPendingTempBanKeepsExpiry(t *testing.T) {
	srv, clock, _ := banTestServer(t, true)
	until := clock.now.Add(time.Hour)
	if _, err := srv.Ban("Griefer", BanOptions{Reason: "griefing", By: audit.Actor{Kind: audit.KindConsole}, Expires: until}); err != nil {
		t.Fatal(err)
	}
	clock.now = clock.now.Add(30 * time.Minute)
	id := client.Identity{Name: "Griefer", XUID: "2535412345678901", UUID: uuid.New(), Authenticated: true}
	if _, ok := srv.conf.Allower.Allow(&net.UDPAddr{IP: net.IPv4(198, 51, 100, 1)}, id, client.Profile{}); ok {
		t.Fatal("the account that joined as the banned name was let in")
	}
	en, ok := srv.bans.players.get(mustXUIDKey(t, "2535412345678901"))
	if !ok || !en.ban.expires.Equal(until) || en.ban.source != "Console" || !en.ban.created.Equal(until.Add(-time.Hour)) {
		t.Errorf("the bound ban: %+v, %v", en, ok)
	}
	clock.now = until
	if _, ok := srv.conf.Allower.Allow(&net.UDPAddr{IP: net.IPv4(198, 51, 100, 1)}, id, client.Profile{}); !ok {
		t.Error("the account was refused after the ban expired")
	}
}

// mustNameKey returns the key of the name passed.
func mustNameKey(t *testing.T, name string) identity.Key {
	t.Helper()
	k, err := identity.OfName(name)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// mustXUIDKey returns the key of the account of the XUID passed.
func mustXUIDKey(t *testing.T, xuid string) identity.Key {
	t.Helper()
	k, err := identity.OfXUID(xuid)
	if err != nil {
		t.Fatal(err)
	}
	return k
}
