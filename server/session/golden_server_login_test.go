package session_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server"
	"github.com/dm-vev/adamant/net/bedrock"
	"github.com/dm-vev/adamant/net/bedrock/login"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/dm-vev/adamant/net/bedrock/wire"
	"github.com/dm-vev/adamant/net/proxy"
	"github.com/google/uuid"
)

// proxySecret is the secret of the proxy links of the login scenario.
var proxySecret = strings.Repeat("golden proxy ", 4)

// TestGoldenServerLogin records the logins a server.Server refuses, and two
// it takes, over the listener the program makes, a NetherNet listener that
// also takes the players of proxies. Its clients log in through a proxy
// link, which names the address they come from: a client not on the
// whitelist, a client banned by name, an operator banned by name, a client
// whose address is banned, and a client whose name the rules of logins
// refuse are each sent what the server refuses them with, or nothing, and
// none becomes a player. An operator not on the whitelist is let in; a
// client on it logs in, gets its game started and spawns.
//
// What is recorded is what each client reads off its connection, decoded
// as a client decodes it, with the normalisations of the harness, but for
// the resource packs offered and stacked, whose UUIDs name the packs. The game
// a client is sent holds the items and the game rules in the order of maps:
// they are sorted by name, and the packet then encoded again, as the line
// of the packet marks.
func TestGoldenServerLogin(t *testing.T) {
	// Each runs in a process of its own, which waits on the clock of its
	// server most of the time.
	t.Parallel()
	if isolated(t) {
		return
	}
	// The address of the links is only known once listening: take a port.
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	proxyAddr := probe.Addr().String()
	_ = probe.Close()
	s := newServerScenario(t, "server_login", serverConfig{brief: true, network: true, user: func(uc *server.UserConfig) {
		uc.Network.Address = "127.0.0.1:0"
		uc.Proxy.Address, uc.Proxy.Secret = proxyAddr, proxySecret
		uc.Whitelist.Enabled = true
	}})
	for _, name := range []string{"Member", "Banned", "Addressed"} {
		if _, err := s.srv.WhitelistAdd(name); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"Operator", "BannedOperator"} {
		if _, err := s.srv.Op(name); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.srv.BanPlayer("banned", "griefing"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.srv.BanPlayer("BannedOperator", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.srv.BanIP("203.0.113.9", "spam"); err != nil {
		t.Fatal(err)
	}
	link, err := proxy.Dial(proxyAddr, []byte(proxySecret))
	if err != nil {
		t.Fatal(err)
	}
	defer link.Close()

	refused := []struct {
		heading, name, addr string
	}{
		{"a client not on the whitelist", "Stranger", "203.0.113.1:4242"},
		{"a client on the whitelist banned by name, of another case", "Banned", "203.0.113.1:4242"},
		{"an operator banned by name", "BannedOperator", "203.0.113.1:4242"},
		{"a client on the whitelist from a banned address", "Addressed", "203.0.113.9:4242"},
		{"a client whose name the rules of logins refuse", "9Lives", "203.0.113.1:4242"},
	}
	for i, r := range refused {
		fmt.Fprintf(&s.out, "== %s: %s from %s\n", r.heading, r.name, r.addr)
		c := s.proxied(link, r.name, r.addr, i)
		c.readAll()
		s.noPlayers()
	}

	s.out.WriteString("== an operator not on the whitelist: Operator from 203.0.113.1:4242\n")
	op := s.proxied(link, "Operator", "203.0.113.1:4242", 10)
	op.readUntil(v2193.IDResourcePacksInfo)
	_ = op.b.Close()
	s.noPlayers()

	s.out.WriteString("== a client on the whitelist: Member from 203.0.113.1:4242\n")
	m := s.proxied(link, "Member", "203.0.113.1:4242", 11)
	m.readUntil(v2193.IDResourcePacksInfo)
	m.send(&v2193.ResourcePackClientResponsePacket{Response: v2193.PackResponseAllPacksDownloaded})
	m.readUntil(v2193.IDResourcePackStack)
	m.send(&v2193.ResourcePackClientResponsePacket{Response: v2193.PackResponseCompleted})
	game := m.readUntil(v2193.IDStartGame, v2193.IDItemRegistry).(*v2193.StartGamePacket)
	m.send(&v2193.RequestChunkRadiusPacket{ChunkRadius: goldenRadius})
	m.readUntil(v2193.IDChunkRadiusUpdated, v2193.IDPlayStatus, v2193.IDCreativeContent)
	m.send(&v2193.SetLocalPlayerAsInitialisedPacket{EntityRuntimeID: game.EntityRuntimeID})
	// What the client is sent from here on is read and left out. The
	// items the server writes once the game started go out with the first
	// flush of the session, and what the session sends goes by the clock
	// of the world. Whether either reaches the client, and whether the
	// player is still online once Accept yielded it, is timing: the
	// listener encodes the batches of Static packets once, compressed,
	// while a proxied connection compresses nothing, so the session of a
	// proxied client ends at the flush that holds the first of them ("batch
	// of another compression"), and what that flush held is lost.
	go m.discard()
	s.awaitSpawn("Member")
	fmt.Fprintf(&s.out, "-- Accept yielded %s\n", s.lastYielded())
	s.out.WriteString("== Member's client disconnects\n")
	_ = m.b.Close()
	s.awaitSave("Member")
	s.providerLog()
	s.noPlayers()
	s.snapshot()
}

// proxiedClient is a client a proxy logged in, as the server's connection
// sees it.
type proxiedClient struct {
	s    *serverScenario
	name string
	b    *bedrock.Conn
	rec  *scenario
}

// proxied opens a client of the name passed on the link, coming from addr,
// with the n-th UUID of the proxied clients.
func (s *serverScenario) proxied(link *proxy.Link, name, addr string, n int) *proxiedClient {
	s.t.Helper()
	id := uuid.MustParse(fmt.Sprintf("00000000-0000-4000-8000-%012x", 0x9000+n))
	identity, _ := json.Marshal(login.Identity{DisplayName: name, Identity: id.String(), XUID: fmt.Sprintf("25354000000%05d", 100+n)})
	data, _ := json.Marshal(login.DefaultClientData("golden.example.invalid:19132", name, v2193.Version))
	p, err := link.Open(proxy.Token{Identity: identity, ClientData: data, Addr: addr, Protocol: v2193.Protocol})
	if err != nil {
		s.t.Fatal(err)
	}
	_ = p.SetReadDeadline(time.Now().Add(20 * time.Second))
	b := bedrock.NewConn(p, bedrock.Config{})
	b.EnableCompression(bedrock.None{})
	return &proxiedClient{s: s, name: name, b: b, rec: &scenario{t: s.t, name: s.name, norm: s.norm, stacks: map[stackSlot]int32{}}}
}

// read reads the next packet the client is sent and records it. It
// returns nil once the connection closed.
func (c *proxiedClient) read() proto.Packet {
	c.s.t.Helper()
	raw, err := c.b.ReadPacket()
	if err != nil {
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			c.s.t.Fatalf("%s: %s: no packet and no close", c.s.name, c.name)
		}
		c.s.out.WriteString("-- the connection closed\n")
		return nil
	}
	h, fields, err := bedrock.ReadHeader(raw)
	if err != nil {
		c.s.t.Fatal(err)
	}
	pk := decodeAs(c.s.t, h.ID, fields)
	mark := false
	switch pk := pk.(type) {
	case *v2193.StartGamePacket:
		slices.SortFunc(pk.GameRules, func(a, b v2193.GameRule) int { return strings.Compare(a.Name, b.Name) })
		mark = true
	case *v2193.ItemRegistryPacket:
		if len(pk.Items) > 1 {
			slices.SortFunc(pk.Items, func(a, b v2193.ItemEntry) int { return strings.Compare(a.Name, b.Name) })
			mark = true
		}
	}
	if mark {
		fields = encode(pk)
	}
	var desc string
	switch pk.(type) {
	case *v2193.ResourcePacksInfoPacket, *v2193.ResourcePackStackPacket:
		// The UUIDs of the packs are those of the packs, not drawn at
		// random: they are left as they are.
		desc = describeAsIs(pk, fields, ">>")
	default:
		desc = c.rec.describe(pk, fields, ">>")
	}
	if mark && !strings.Contains(strings.SplitN(desc, "\n", 2)[0], " normalised") {
		first, rest, _ := strings.Cut(desc, "\n")
		desc = first + " normalised\n" + rest
	}
	if len(fields) > maxHexBytes {
		desc = briefPacket(pk, desc)
	}
	c.s.out.WriteString(desc)
	return pk
}

// describeAsIs describes a packet as the harness's describe does, without
// normalising it.
func describeAsIs(pk proto.Packet, raw []byte, dir string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s id=%d len=%d\n", dir, packetName(pk), pk.ID(), len(raw))
	writeValue(&b, "    ", "", reflect.ValueOf(pk).Elem())
	for off := 0; off < len(raw); off += 32 {
		fmt.Fprintf(&b, "    | %x\n", raw[off:min(off+32, len(raw))])
	}
	return b.String()
}

// briefPacket shortens the rendering of a large packet the client logged in
// is sent: StartGame keeps its fields but for the blocks, which it gives the
// number and the SHA-256 of the rendering of; other packets keep the line of
// the packet and that of the SHA-256 of their encoding.
func briefPacket(pk proto.Packet, desc string) string {
	lines := strings.Split(strings.TrimSuffix(desc, "\n"), "\n")
	game, ok := pk.(*v2193.StartGamePacket)
	if !ok {
		return lines[0] + "\n" + lines[len(lines)-1] + "\n"
	}
	var blocks strings.Builder
	writeValue(&blocks, "", "", reflect.ValueOf(game.Blocks))
	short := *game
	short.Blocks = nil
	var b strings.Builder
	writeValue(&b, "    ", "", reflect.ValueOf(short))
	rendered := strings.Replace(b.String(), "      Blocks: []\n", fmt.Sprintf("      Blocks: (%d) rendered sha256 %x\n", len(game.Blocks), sha256Of(blocks.String())), 1)
	return lines[0] + "\n" + rendered + lines[len(lines)-1] + "\n"
}

// readUntil reads what the client is sent until a packet of each ID
// passed, in order, was read, and returns the first of them.
func (c *proxiedClient) readUntil(ids ...uint32) proto.Packet {
	c.s.t.Helper()
	var first proto.Packet
	for _, id := range ids {
		for {
			pk := c.read()
			if pk == nil {
				c.s.t.Fatalf("%s: %s: the connection closed before packet %d", c.s.name, c.name, id)
			}
			if pk.ID() == id {
				if first == nil {
					first = pk
				}
				break
			}
		}
	}
	return first
}

// readAll reads what the client is sent until the connection closes.
func (c *proxiedClient) readAll() {
	c.s.t.Helper()
	for c.read() != nil {
	}
}

// discard reads what the client is sent without recording it until the
// connection closes.
func (c *proxiedClient) discard() {
	for {
		if _, err := c.b.ReadPacket(); err != nil {
			return
		}
	}
}

// send sends a packet as the client does, and records it.
func (c *proxiedClient) send(pk proto.Packet) {
	c.s.t.Helper()
	w := wire.Writer{B: bedrock.AppendHeader(nil, bedrock.Header{ID: pk.ID()})}
	pk.Encode(&w)
	raw := encode(pk)
	c.s.out.WriteString(c.rec.describe(decodeAs(c.s.t, pk.ID(), raw), raw, "<<"))
	if err := c.b.WritePacket(w.B); err != nil {
		c.s.t.Fatal(err)
	}
	if err := c.b.Flush(); err != nil {
		c.s.t.Fatal(err)
	}
}

// noPlayers records that the server has no player, failing if it has one
// or if Accept yielded one.
func (s *serverScenario) noPlayers() {
	s.t.Helper()
	select {
	case h := <-s.spawned:
		s.t.Fatalf("%s: the server spawned %v", s.name, h)
	default:
	}
	if n := s.srv.PlayerCount(); n != 0 {
		s.t.Fatalf("%s: %d players online", s.name, n)
	}
	s.out.WriteString("-- server\n    players online: 0\n")
}
