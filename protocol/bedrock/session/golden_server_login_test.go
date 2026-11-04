package session_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/protocol/bedrock"
	"github.com/df-mc/dragonfly/server"
	"github.com/dm-vev/adamant/net/bedrock"
	"github.com/dm-vev/adamant/net/bedrock/login"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
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
// client on it logs in, gets its game started and spawns, and what its
// session sends it is recorded as the scenarios of golden_server_test.go
// record theirs: what the spawn sent up to the marker Accept writes, then
// the ticks after until its chunks and the abilities sent again are in.
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
		uc.Whitelist.Enabled = true
	}, bedrock: func(uc *bedrockadapter.UserConfig) {
		uc.Network.Address = "127.0.0.1:0"
		uc.Proxy.Address, uc.Proxy.Secret = proxyAddr, proxySecret
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
	op.readUntil(mc.IDResourcePacksInfo)
	_ = op.b.Close()
	s.noPlayers()

	s.out.WriteString("== a client on the whitelist: Member from 203.0.113.1:4242\n")
	m := s.proxied(link, "Member", "203.0.113.1:4242", 11)
	m.readUntil(mc.IDResourcePacksInfo)
	m.send(&mc.ResourcePackClientResponsePacket{Response: mc.PackResponseAllPacksDownloaded})
	m.readUntil(mc.IDResourcePackStack)
	m.send(&mc.ResourcePackClientResponsePacket{Response: mc.PackResponseCompleted})
	game := m.readUntil(mc.IDStartGame, mc.IDItemRegistry).(*mc.StartGamePacket)
	m.send(&mc.RequestChunkRadiusPacket{ChunkRadius: goldenRadius})
	m.readUntil(mc.IDChunkRadiusUpdated, mc.IDPlayStatus, mc.IDCreativeContent)
	// From here on the client is recorded as those of the test's listener
	// are: what the spawn sent it up to the marker Accept writes, then what
	// the ticks after sent up to a marker of a step.
	member := m.spawning()
	m.send(&mc.SetLocalPlayerAsInitialisedPacket{EntityRuntimeID: game.EntityRuntimeID})
	s.awaitSpawn("Member")
	fmt.Fprintf(&s.out, "-- Accept yielded %s\n", s.lastYielded())
	s.spawnSent(member, nil)
	s.out.WriteString("== Member's client disconnects\n")
	_ = m.b.Close()
	member.left()
	s.awaitSave("Member")
	s.providerLog()
	s.noPlayers()
	s.snapshot()
}

// proxiedClient is a client a proxy logged in, as the server's connection
// sees it.
type proxiedClient struct {
	s    *serverScenario
	who  serverIdentity
	name string
	b    *bedrock.Conn
	rec  *scenario
}

// proxied opens a client of the name passed on the link, coming from addr,
// with the n-th UUID of the proxied clients.
func (s *serverScenario) proxied(link *proxy.Link, name, addr string, n int) *proxiedClient {
	s.t.Helper()
	return s.proxiedAs(link, name, addr, n, false)
}

// proxiedAs is proxied of a client whose proxy says Xbox Live vouched for
// it if xboxLive.
func (s *serverScenario) proxiedAs(link *proxy.Link, name, addr string, n int, xboxLive bool) *proxiedClient {
	s.t.Helper()
	id := uuid.MustParse(fmt.Sprintf("00000000-0000-4000-8000-%012x", 0x9000+n))
	xuid := fmt.Sprintf("25354000000%05d", 100+n)
	identity, _ := json.Marshal(login.Identity{DisplayName: name, Identity: id.String(), XUID: xuid})
	cd := login.DefaultClientData("golden.example.invalid:19132", name, goldenProtocol.Version)
	// The default is drawn at random; the player list shows it.
	cd.PlayFabID = fmt.Sprintf("%016x", 0x9000+n)
	data, _ := json.Marshal(cd)
	p, err := link.Open(proxy.Token{Identity: identity, ClientData: data, XboxLive: xboxLive, Addr: addr, Protocol: goldenProtocol.Number})
	if err != nil {
		s.t.Fatal(err)
	}
	_ = p.SetReadDeadline(time.Now().Add(20 * time.Second))
	b := bedrock.NewConn(p, bedrock.Config{})
	b.EnableCompression(bedrock.None{})
	host, port, _ := net.SplitHostPort(addr)
	portNum, _ := strconv.Atoi(port)
	who := serverIdentity{name: name, xuid: xuid, id: id, addr: &net.UDPAddr{IP: net.ParseIP(host), Port: portNum}}
	return &proxiedClient{s: s, who: who, name: name, b: b, rec: &scenario{t: s.t, name: s.name, norm: s.norm, stacks: map[stackSlot]int32{}}}
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
	model := asModel(c.s.t, h.ID, fields)
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
	return model
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

// spawning makes the client one of the scenario, as those of the test's
// listener are, before its player spawns: what it is sent from now on is
// read off its connection, decoded as a client decodes it, and handed to
// a serverClient, whose player Accept writes its marker to.
func (c *proxiedClient) spawning() *serverClient {
	sc := &serverClient{s: c.s, who: c.who, conn: &identityConn{goldenConn: newGoldenConn(), who: c.who}, rec: c.rec}
	c.s.mu.Lock()
	c.s.clients = append(c.s.clients, sc)
	c.s.mu.Unlock()
	go func() {
		defer sc.conn.Close()
		for {
			raw, err := c.b.ReadPacket()
			if err != nil {
				return
			}
			h, fields, err := bedrock.ReadHeader(raw)
			if err != nil {
				c.s.t.Errorf("%s: %s: %v", c.s.name, c.name, err)
				return
			}
			pk, ok := readWire(h.ID, fields)
			if !ok {
				c.s.t.Errorf("%s: %s: packet %d does not decode", c.s.name, c.name, h.ID)
				return
			}
			sc.conn.written <- pk
		}
	}()
	return sc
}

// send sends a packet as the client does, and records it.
func (c *proxiedClient) send(pk proto.Packet) {
	c.s.t.Helper()
	for _, pk := range toWire(pk) {
		w := wire.Writer{B: bedrock.AppendHeader(nil, bedrock.Header{ID: pk.ID()})}
		pk.Encode(&w)
		raw := encode(pk)
		c.s.out.WriteString(c.rec.describe(decodeAs(c.s.t, pk.ID(), raw), raw, "<<"))
		if err := c.b.WritePacket(w.B); err != nil {
			c.s.t.Fatal(err)
		}
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
