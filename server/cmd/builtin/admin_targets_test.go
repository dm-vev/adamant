package builtin

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/audit"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/identity"
)

// targetAdapter is a server that resolves the players an admin names as
// it is told, and records the lists it changes.
type targetAdapter struct {
	serverAdapter
	target  server.Target
	err     error
	changed bool
	calls   *[]string
}

func (s targetAdapter) ResolvePlayer(string) (server.Target, error) { return s.target, s.err }
func (s targetAdapter) ResolveEntry(server.List, string) (server.Target, error) {
	return s.target, s.err
}
func (s targetAdapter) record(call string) (bool, error) {
	*s.calls = append(*s.calls, call)
	return s.changed, nil
}
func (s targetAdapter) Op(name string) (bool, error) { return s.record("Op " + name) }
func (targetAdapter) OpLevel() cmd.Level             { return cmd.LevelOwner }
func (targetAdapter) OperatorLevel(string) (cmd.Level, bool) {
	return cmd.LevelOwner, true
}
func (s targetAdapter) Deop(name string) (bool, error) { return s.record("Deop " + name) }
func (s targetAdapter) BanPlayer(name, reason string) (bool, error) {
	return s.record("BanPlayer " + name + " " + reason)
}
func (s targetAdapter) Ban(name string, o server.BanOptions) (server.BanChange, error) {
	if changed, _ := s.record("Ban " + name + " " + o.Reason + " by " + o.By.String()); changed {
		return server.BanAdded, nil
	}
	return server.BanUnchanged, nil
}
func (targetAdapter) ActorOf(cmd.Source) audit.Actor { return audit.Actor{Kind: audit.KindConsole} }
func (s targetAdapter) PardonPlayer(name string) (bool, error) {
	return s.record("PardonPlayer " + name)
}
func (s targetAdapter) WhitelistAdd(name string) (bool, error) {
	return s.record("WhitelistAdd " + name)
}
func (s targetAdapter) WhitelistRemove(name string) (bool, error) {
	return s.record("WhitelistRemove " + name)
}

// TestAdminCommandsNameTheirTargets checks what the commands that change
// the lists print for an account, a name held for the first account that
// joins with it, a name of a server keyed by name, and a name the server
// cannot resolve, and that they pass the server the account by its XUID.
func TestAdminCommandsNameTheirTargets(t *testing.T) {
	steve, _ := identity.OfXUID("2535412345678901")
	held, _ := identity.OfName("Steve")
	account := server.Target{Key: steve, Name: "Steve", Found: server.FoundKnown}
	pending := server.Target{Key: held, Name: "Steve", Found: server.FoundPending, Pending: true}
	byName := server.Target{Key: held, Name: "Steve", Found: server.FoundByName}
	var b strings.Builder
	for _, c := range []struct {
		name    string
		command func(serverAdapter) string
	}{
		{"op", func(s serverAdapter) string { return outputText(runTestCommand(newOpCommand(s), "Steve", nil)) }},
		{"deop", func(s serverAdapter) string { return outputText(runTestCommand(newDeopCommand(s), "Steve", nil)) }},
		{"ban", func(s serverAdapter) string {
			return outputText(runTestCommand(newBanPlayerCommand(s), "Steve griefing", nil))
		}},
		{"pardon", func(s serverAdapter) string { return outputText(runTestCommand(newPardonCommand(s), "Steve", nil)) }},
		{"whitelist add", func(s serverAdapter) string {
			return outputText(runTestCommand(newWhitelistCommand(s), "add Steve", nil))
		}},
		{"whitelist remove", func(s serverAdapter) string {
			return outputText(runTestCommand(newWhitelistCommand(s), "remove Steve", nil))
		}},
	} {
		for _, tc := range []struct {
			desc    string
			target  server.Target
			err     error
			changed bool
		}{
			{"account", account, nil, true},
			{"account, unchanged", account, nil, false},
			{"held by name", pending, nil, true},
			{"by name", byName, nil, true},
			{"unresolved", server.Target{}, errors.New("several entries of the list had the name Steve: name one with xuid:<XUID>"), true},
		} {
			var calls []string
			out := c.command(targetAdapter{target: tc.target, err: tc.err, changed: tc.changed, calls: &calls})
			fmt.Fprintf(&b, "%s, %s: %q %q\n", c.name, tc.desc, strings.TrimSpace(out), calls)
		}
	}
	want := `op, account: "Made Steve (xuid:2535412345678901) a server operator." ["Op xuid:2535412345678901"]
op, account, unchanged: "Steve (xuid:2535412345678901) is already an operator." ["Op xuid:2535412345678901"]
op, held by name: "Steve has not joined this server: the first Xbox account that joins as Steve becomes an operator. Name the account with xuid:<XUID> instead." ["Op Steve"]
op, by name: "Made Steve a server operator." ["Op Steve"]
op, unresolved: "several entries of the list had the name Steve: name one with xuid:<XUID>" []
deop, account: "Removed Steve (xuid:2535412345678901) from the server operators." ["Deop xuid:2535412345678901"]
deop, account, unchanged: "Steve (xuid:2535412345678901) is not an operator." ["Deop xuid:2535412345678901"]
deop, held by name: "Removed Steve from the server operators." ["Deop Steve"]
deop, by name: "Removed Steve from the server operators." ["Deop Steve"]
deop, unresolved: "several entries of the list had the name Steve: name one with xuid:<XUID>" []
ban, account: "Banned Steve (xuid:2535412345678901): griefing" ["Ban xuid:2535412345678901 griefing by Console"]
ban, account, unchanged: "Nothing changed. The player is already banned" ["Ban xuid:2535412345678901 griefing by Console"]
ban, held by name: "Steve has not joined this server: the first Xbox account that joins as Steve is banned. Name the account with xuid:<XUID> instead." ["Ban Steve griefing by Console"]
ban, by name: "Banned Steve: griefing" ["Ban Steve griefing by Console"]
ban, unresolved: "several entries of the list had the name Steve: name one with xuid:<XUID>" []
pardon, account: "Unbanned Steve (xuid:2535412345678901)" ["PardonPlayer xuid:2535412345678901"]
pardon, account, unchanged: "Nothing changed. The player isn't banned" ["PardonPlayer xuid:2535412345678901"]
pardon, held by name: "Unbanned Steve" ["PardonPlayer Steve"]
pardon, by name: "Unbanned Steve" ["PardonPlayer Steve"]
pardon, unresolved: "several entries of the list had the name Steve: name one with xuid:<XUID>" []
whitelist add, account: "Added Steve (xuid:2535412345678901) to the whitelist." ["WhitelistAdd xuid:2535412345678901"]
whitelist add, account, unchanged: "Steve (xuid:2535412345678901) is already on the whitelist." ["WhitelistAdd xuid:2535412345678901"]
whitelist add, held by name: "Steve has not joined this server: the first Xbox account that joins as Steve is on the whitelist. Name the account with xuid:<XUID> instead." ["WhitelistAdd Steve"]
whitelist add, by name: "Added Steve to the whitelist." ["WhitelistAdd Steve"]
whitelist add, unresolved: "several entries of the list had the name Steve: name one with xuid:<XUID>" []
whitelist remove, account: "Removed Steve (xuid:2535412345678901) from the whitelist." ["WhitelistRemove xuid:2535412345678901"]
whitelist remove, account, unchanged: "Steve (xuid:2535412345678901) is not on the whitelist." ["WhitelistRemove xuid:2535412345678901"]
whitelist remove, held by name: "Removed Steve from the whitelist." ["WhitelistRemove Steve"]
whitelist remove, by name: "Removed Steve from the whitelist." ["WhitelistRemove Steve"]
whitelist remove, unresolved: "several entries of the list had the name Steve: name one with xuid:<XUID>" []
`
	if got := b.String(); got != want {
		t.Errorf("outputs:\n%s\nwant:\n%s", got, want)
	}
}

// outputText returns the messages and the errors of a command output, a
// line each.
func outputText(o *cmd.Output) string {
	var lines []string
	for _, m := range o.Messages() {
		lines = append(lines, m.String())
	}
	for _, err := range o.Errors() {
		lines = append(lines, err.Error())
	}
	return strings.Join(lines, "\n")
}

// banListAdapter is a server of the bans it holds.
type banListAdapter struct {
	serverAdapter
	players, ips []server.BanInfo
}

func (a banListAdapter) BanList(ips bool) ([]server.BanInfo, error) {
	if ips {
		return a.ips, nil
	}
	return a.players, nil
}

// TestBanlistOutput checks /banlist in Java's format: the count, then a
// line a ban, "was banned by" its source, its reason, and the end of a
// ban for a time; both kinds by default, or the kind named.
func TestBanlistOutput(t *testing.T) {
	a := banListAdapter{
		players: []server.BanInfo{
			{Target: "Griefer (xuid:2535412345678901)", Source: "Steve (xuid:2535498765432109)", Reason: "griefing", Until: "2026-11-11 at 14:20:31 UTC"},
			{Target: "steve", Source: "(Unknown)"},
		},
		ips: []server.BanInfo{{Target: "203.0.113.7", IP: true, Source: "Console", Reason: "alts"}},
	}
	c := newBanListCommand(a)
	for args, want := range map[string]string{
		"": "There are 3 ban(s):\nGriefer (xuid:2535412345678901) was banned by Steve (xuid:2535498765432109): griefing (until 2026-11-11 at 14:20:31 UTC)\n" +
			"steve was banned by (Unknown)\n203.0.113.7 was banned by Console: alts",
		"ips":     "There are 1 ban(s):\n203.0.113.7 was banned by Console: alts",
		"players": "There are 2 ban(s):\nGriefer (xuid:2535412345678901) was banned by Steve (xuid:2535498765432109): griefing (until 2026-11-11 at 14:20:31 UTC)\nsteve was banned by (Unknown)",
	} {
		if got := strings.TrimSpace(outputText(runTestCommand(c, args, nil))); got != want {
			t.Errorf("/banlist %s:\n%s\nwant\n%s", args, got, want)
		}
	}
	if got := strings.TrimSpace(outputText(runTestCommand(newBanListCommand(banListAdapter{}), "", nil))); got != "There are no bans" {
		t.Errorf("/banlist of none: %q", got)
	}
}
