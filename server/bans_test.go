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
	if ok, err := srv.Ban("Griefer", BanOptions{Reason: "griefing", By: steve, Expires: until}); ok != BanAdded || err != nil {
		t.Fatalf("Ban = %v, %v", ok, err)
	}
	if ok, err := srv.BanPlayer("Old", "spam"); !ok || err != nil {
		t.Fatalf("BanPlayer = %v, %v", ok, err)
	}
	if ok, err := srv.BanAddress("203.0.113.7", BanOptions{Reason: "alts"}); ok != BanAdded || err != nil {
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
	want := "You are banned from this server.\nReason: griefing\nYour ban will be removed on " + until.Local().Format("2006-01-02 at 15:04:05 MST")
	if msg, ok := srv.conf.Allower.Allow(addr, client.Identity{Name: "Griefer", UUID: uuid.New()}, client.Profile{}); ok || msg != want {
		t.Errorf("Griefer at login: %q, %v, want %q", msg, ok, want)
	}
	if msg, _ := srv.conf.Allower.Allow(addr, client.Identity{Name: "Old", UUID: uuid.New()}, client.Profile{}); msg != "You are banned from this server.\nReason: spam" {
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

// TestBanDurations checks the durations of bans: Go's units plus d and w,
// in any combination, a minute at least.
func TestBanDurations(t *testing.T) {
	for _, tc := range []struct {
		s    string
		want time.Duration
	}{
		{"90m", 90 * time.Minute}, {"12h", 12 * time.Hour}, {"7d", 7 * 24 * time.Hour}, {"1w2d", 9 * 24 * time.Hour},
		{"1d12h30m", 36*time.Hour + 30*time.Minute}, {" 2W ", 14 * 24 * time.Hour}, {"1.5h", 90 * time.Minute}, {"60s", time.Minute},
	} {
		if d, err := ParseBanDuration(tc.s); err != nil || d != tc.want {
			t.Errorf("%q = %v, %v, want %v", tc.s, d, err, tc.want)
		}
	}
	for _, s := range []string{"0", "0m", "-1h", "5s", "59s", "", "7", "week", "1y", "1d-2h"} {
		if d, err := ParseBanDuration(s); err == nil {
			t.Errorf("%q = %v, want an error", s, d)
		}
	}
}

// TestBanOverBan checks a ban over a ban: for ever over for a time makes
// it for ever; for a time replaces the expiry and the reason; for ever
// over for ever changes nothing; a ban for a time above MaxDuration, or
// under a minute, is refused.
func TestBanOverBan(t *testing.T) {
	srv, clock, _ := banTestServer(t, false)
	srv.bans.maxDuration = 30 * 24 * time.Hour
	day := 24 * time.Hour
	steps := []struct {
		o    BanOptions
		want BanChange
		err  bool
	}{
		{BanOptions{Reason: "griefing", For: 7 * day}, BanAdded, false},
		{BanOptions{Reason: "again", For: 14 * day}, BanUpdated, false},
		{BanOptions{}, BanMadePermanent, false},
		{BanOptions{Reason: "still"}, BanUnchanged, false},
		{BanOptions{For: 3 * day}, BanUpdated, false},
		{BanOptions{For: 31 * day}, BanUnchanged, true},
		{BanOptions{For: 30 * time.Second}, BanUnchanged, true},
	}
	for i, s := range steps {
		got, err := srv.Ban("Griefer", s.o)
		if got != s.want || (err != nil) != s.err {
			t.Errorf("step %d: %v, %v, want %v, error %v", i, got, err, s.want, s.err)
		}
	}
	bans, _ := srv.BanList(false)
	if len(bans) != 1 || bans[0].Reason != "again" || !bans[0].Expires.Equal(clock.now.Add(3*day)) || bans[0].Source != "API" {
		t.Errorf("the ban after its steps: %+v", bans)
	}
	if got, _ := srv.BanAddress("203.0.113.7", BanOptions{For: day}); got != BanAdded {
		t.Errorf("an address banned for a day: %v", got)
	}
	if got, _ := srv.BanAddress("203.0.113.7", BanOptions{}); got != BanMadePermanent {
		t.Errorf("the address banned for ever: %v", got)
	}
}

// TestIPBanPrefixes checks the bans of addresses and networks: an IPv4
// address alone; an IPv6 address as its /64 by default, and alone at 128;
// a network as typed; a loopback address banned only by a ban of it alone;
// an address an older server wrote alone matching itself alone; and
// pardoning an IPv6 address pardons its network.
func TestIPBanPrefixes(t *testing.T) {
	srv, _, _ := banTestServer(t, false)
	banned := func(addr string) bool {
		_, ok := srv.Banned(addr, "")
		return ok
	}
	for _, target := range []string{"203.0.113.7", "2001:db8:1:2::5", "198.51.100.0/24"} {
		if _, err := srv.BanAddress(target, BanOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	for addr, want := range map[string]bool{
		"203.0.113.7:4242": true, "203.0.113.8": false,
		"[2001:db8:1:2::99]:19132": true, "2001:db8:1:3::1": false,
		"198.51.100.200": true, "198.51.101.1": false,
		"::ffff:203.0.113.7": true,
	} {
		if got := banned(addr); got != want {
			t.Errorf("%s banned %v, want %v", addr, got, want)
		}
	}
	if bans, _ := srv.BanList(true); len(bans) != 3 || bans[1].Target != "2001:db8:1:2::/64" {
		t.Errorf("bans of addresses %+v", bans)
	}

	if _, err := srv.BanAddress("0.0.0.0/0", BanOptions{}); err != nil {
		t.Fatal(err)
	}
	if banned("127.0.0.1:19132") || !banned("192.0.2.1") {
		t.Error("a ban of every IPv4 address bans loopback, or not the others")
	}
	if _, err := srv.BanAddress("127.0.0.1", BanOptions{}); err != nil || !banned("127.0.0.1") {
		t.Errorf("a ban of 127.0.0.1 alone: %v, banned %v", err, banned("127.0.0.1"))
	}

	if removed, err := srv.PardonIP("2001:db8:1:2::5"); !removed || err != nil {
		t.Errorf("pardoning the IPv6 address: %v, %v", removed, err)
	}
	if banned("2001:db8:1:2::99") {
		t.Error("the network of the IPv6 address is still banned")
	}

	srv.bans.v6Prefix = 128
	if _, err := srv.BanAddress("2001:db8::1", BanOptions{}); err != nil {
		t.Fatal(err)
	}
	if !banned("2001:db8::1") || banned("2001:db8::2") {
		t.Error("IPv6Prefix 128 bans more or less than the address")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "banned-players.json")
	if err := os.WriteFile(path, []byte(`{"players":[],"ips":[{"ip":"2001:db8::5","reason":"old"}]}`), 0644); err != nil {
		t.Fatal(err)
	}
	b, err := loadBans(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := b.bannedIPLocked("2001:db8::5"); !ok {
		t.Error("the address of an older file is not banned")
	}
	if _, ok, _ := b.bannedIPLocked("2001:db8::6"); ok {
		t.Error("the address of an older file bans its network")
	}
}
