package server_test

import (
	"strings"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/text"
	"github.com/df-mc/dragonfly/server/world"
)

// TestBanKickCarriesReason checks that a player online who is banned is
// kicked with the text it would be refused with at login, the reason
// included, where the kick named no reason before.
func TestBanKickCarriesReason(t *testing.T) {
	a := newAuditTest(t)
	_, j := a.join(t, "Griefer")
	if ok, err := a.srv.Ban("Griefer", server.BanOptions{Reason: "griefing"}); ok != server.BanAdded || err != nil {
		t.Fatalf("Ban = %v, %v", ok, err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if calls := j.Recorder().CallsTo("Disconnect"); len(calls) > 0 {
			if got := calls[0].Args[0].(text.Text).Plain(); got != "You are banned from this server.\nReason: griefing" {
				t.Errorf("kicked with %q", got)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the banned player was not kicked")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestBanIPOfPlayerOnline checks /ban-ip of a player online: the address
// of the player is banned, the output names every player online from it,
// and each is kicked.
func TestBanIPOfPlayerOnline(t *testing.T) {
	a := newAuditTest(t)
	_, alt1 := a.join(t, "Alt1")
	_, alt2 := a.join(t, "Alt2")
	out := &cmd.Output{}
	<-a.srv.World().Exec(func(tx *world.Tx) { cmd.ExecuteLine(consoleLike{out}, "/ban-ip Alt1 alts", tx, nil) })
	var lines []string
	for _, m := range out.Messages() {
		lines = append(lines, m.String())
	}
	if got := strings.Join(lines, "\n"); got != "Banned IP 203.0.113.9: alts\nThis ban affects 2 player(s): Alt1, Alt2" {
		t.Errorf("/ban-ip Alt1: %q, errors %v", got, out.Errors())
	}
	deadline := time.Now().Add(10 * time.Second)
	for len(alt1.Recorder().CallsTo("Disconnect")) == 0 || len(alt2.Recorder().CallsTo("Disconnect")) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the players of the address were not kicked")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestTempBanCommands checks /tempban and /tempban-ip: a ban for the time
// typed, told with its end as [Bans] TimeFormat writes it; a ban for a
// time over a ban moves its end; a duration under a minute or over
// MaxDuration is refused; an IPv6 address bans its /64; level 2 is told
// the commands do not exist.
func TestTempBanCommands(t *testing.T) {
	a := newAuditTest(t, func(uc *server.UserConfig) { uc.Bans.MaxDuration = "30d" })
	day := 24 * time.Hour
	until := func(target string, ip bool, d time.Duration) string {
		t.Helper()
		b, ok := a.srv.BanOf(target, ip)
		if !ok {
			t.Fatalf("%s is not banned", target)
		}
		if got := b.Expires.Sub(b.Created); got != d {
			t.Errorf("the ban of %s lasts %v, want %v", target, got, d)
		}
		return b.Until
	}
	for _, s := range []struct {
		level  cmd.Level
		line   string
		want   func() string
		target string
		ip     bool
	}{
		{cmd.LevelAdmin, "/tempban Griefer 7d griefing", func() string {
			return "Banned Griefer until " + until("Griefer", false, 7*day) + ": griefing"
		}, "Griefer", false},
		{cmd.LevelAdmin, "/tempban Griefer 1w2d again", func() string {
			return "The ban of Griefer now ends on " + until("Griefer", false, 9*day) + "."
		}, "Griefer", false},
		{cmd.LevelAdmin, "/tempban Spammer 90m", func() string {
			return "Banned Spammer until " + until("Spammer", false, 90*time.Minute) + "."
		}, "Spammer", false},
		{cmd.LevelAdmin, "/tempban Quick 5s", func() string {
			return `error: duration "5s": a ban lasts a minute at least`
		}, "", false},
		{cmd.LevelAdmin, "/tempban Long 31d", func() string {
			return "error: a ban lasts 720h0m0s at most ([Bans] MaxDuration), not 744h0m0s"
		}, "", false},
		{cmd.LevelAdmin, "/tempban-ip 203.0.113.7 12h alts", func() string {
			return "Banned IP 203.0.113.7 until " + until("203.0.113.7", true, 12*time.Hour) + ": alts"
		}, "203.0.113.7", true},
		{cmd.LevelAdmin, "/tempban-ip 2001:db8:1:2::5 1d", func() string {
			return "Banned IP 2001:db8:1:2::/64 until " + until("2001:db8:1:2::/64", true, day) + "."
		}, "2001:db8:1:2::/64", true},
		{cmd.LevelAdmin, "/tempban-ip 203.0.113.7 2d", func() string {
			return "The ban of IP 203.0.113.7 now ends on " + until("203.0.113.7", true, 2*day) + "."
		}, "203.0.113.7", true},
		{cmd.LevelGameMaster, "/tempban Other 1d", func() string {
			return "error: " + cmd.MessageUnknown.F("tempban").Error()
		}, "", false},
	} {
		if got, want := outputLines(run(t, s.level, s.line)), s.want(); got != want {
			t.Errorf("%s from level %v:\n%s\nwant\n%s", s.line, s.level, got, want)
		}
	}
	if _, ok := a.srv.BanOf("Quick", false); ok {
		t.Error("a ban of 5 seconds was made")
	}
}
