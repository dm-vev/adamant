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
func (s targetAdapter) Ban(name string, o server.BanOptions) (bool, error) {
	return s.record("Ban " + name + " " + o.Reason + " by " + o.By.String())
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
ban, account: "Banned Steve (xuid:2535412345678901)." ["Ban xuid:2535412345678901 griefing by Console"]
ban, account, unchanged: "Steve (xuid:2535412345678901) is already banned." ["Ban xuid:2535412345678901 griefing by Console"]
ban, held by name: "Steve has not joined this server: the first Xbox account that joins as Steve is banned. Name the account with xuid:<XUID> instead." ["Ban Steve griefing by Console"]
ban, by name: "Banned Steve." ["Ban Steve griefing by Console"]
ban, unresolved: "several entries of the list had the name Steve: name one with xuid:<XUID>" []
pardon, account: "Unbanned Steve (xuid:2535412345678901)." ["PardonPlayer xuid:2535412345678901"]
pardon, account, unchanged: "Steve (xuid:2535412345678901) is not banned." ["PardonPlayer xuid:2535412345678901"]
pardon, held by name: "Unbanned Steve." ["PardonPlayer Steve"]
pardon, by name: "Unbanned Steve." ["PardonPlayer Steve"]
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
