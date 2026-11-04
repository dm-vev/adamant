package session_test

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/protocol/bedrock"
	"github.com/df-mc/dragonfly/server"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/dm-vev/adamant/net/proxy"
)

// TestGoldenServerLoginXUID records the logins a server.Server keyed by
// XUID refuses and takes, over the listener the program makes, through a
// proxy link whose tokens say whether Xbox Live vouched for each client.
// The XUID of the n-th proxied client is 25354000000 and 100+n, as
// proxied gives it. The lists hold the account of a member, the account of
// an operator off the whitelist, a banned account, and an operator held by
// the name Pending. A client nobody vouched for is refused, though it names
// the XUID of the member; an account banned, and another account named as
// the operator, are refused; the operator, the account that joins as
// Pending and the member are let in, which the scenario stops at the
// resource packs. The files of the lists are recorded at the end: the
// entry of Pending names the account that joined with it.
//
// The adapter of the scenario has Xbox Live sign-in off, so that the
// listener needs no keys of the service: only the tokens of the proxy
// vouch for the clients, and the server itself refuses those that do not.
func TestGoldenServerLoginXUID(t *testing.T) {
	// Each runs in a process of its own, which waits on the clock of its
	// server most of the time.
	t.Parallel()
	if isolated(t) {
		return
	}
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	proxyAddr := probe.Addr().String()
	_ = probe.Close()
	s := newServerScenario(t, "server_login_xuid", serverConfig{brief: true, network: true, user: func(uc *server.UserConfig) {
		uc.Whitelist.Enabled = true
		uc.Server.IdentityKey = "xuid"
	}, bedrock: func(uc *bedrockadapter.UserConfig) {
		uc.Network.Address = "127.0.0.1:0"
		uc.Proxy.Address, uc.Proxy.Secret = proxyAddr, proxySecret
	}})
	xuid := func(n int) string { return fmt.Sprintf("xuid:25354000000%05d", 100+n) }
	if _, err := s.srv.WhitelistAdd(xuid(1)); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{xuid(2), "Pending"} {
		if _, err := s.srv.Op(name); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.srv.BanPlayer(xuid(3), "griefing"); err != nil {
		t.Fatal(err)
	}
	link, err := proxy.Dial(proxyAddr, []byte(proxySecret))
	if err != nil {
		t.Fatal(err)
	}
	defer link.Close()

	refused := []struct {
		heading, name string
		n             int
		xboxLive      bool
	}{
		{"a client nobody vouched for, with the XUID of the member", "Member", 1, false},
		{"a banned account", "Banned", 3, true},
		{"another account named as the operator", "Operator", 4, true},
		{"an account not on the whitelist", "Stranger", 5, true},
	}
	for _, r := range refused {
		fmt.Fprintf(&s.out, "== %s: %s, %s, Xbox Live %t\n", r.heading, r.name, xuid(r.n), r.xboxLive)
		c := s.proxiedAs(link, r.name, "203.0.113.1:4242", r.n, r.xboxLive)
		c.readAll()
		s.noPlayers()
	}
	admitted := []struct {
		heading, name string
		n             int
	}{
		{"the operator, off the whitelist, under another name", "Renamed", 2},
		{"the first account that joins as Pending", "Pending", 6},
		{"the member", "Member", 1},
	}
	for _, a := range admitted {
		fmt.Fprintf(&s.out, "== %s: %s, %s\n", a.heading, a.name, xuid(a.n))
		c := s.proxiedAs(link, a.name, "203.0.113.1:4242", a.n, true)
		c.readUntil(v2193.IDResourcePacksInfo)
		_ = c.b.Close()
		s.noPlayers()
	}
	s.out.WriteString("== another account joins as Pending: Pending, " + xuid(7) + "\n")
	c := s.proxiedAs(link, "Pending", "203.0.113.1:4242", 7, true)
	c.readAll()
	s.noPlayers()

	for _, name := range []string{"white-list.txt", "ops.json", "banned-players.json"} {
		data, err := os.ReadFile(filepath.Join(s.dir, name))
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&s.out, "-- %s\n", name)
		for _, line := range strings.SplitAfter(string(data), "\n") {
			if line != "" {
				fmt.Fprintf(&s.out, "    %q\n", line)
			}
		}
	}
	s.snapshot()
}
