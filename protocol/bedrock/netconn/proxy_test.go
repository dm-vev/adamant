package netconn

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/protocol/bedrock/internal/versions"
	"github.com/dm-vev/adamant/net/bedrock"
	ourclient "github.com/dm-vev/adamant/net/bedrock/client"
	ourlogin "github.com/dm-vev/adamant/net/bedrock/login"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/dm-vev/adamant/net/bedrock/wire"
	"github.com/dm-vev/adamant/net/proxy"
	"github.com/google/uuid"
)

var proxySecret = []byte("a secret the proxy and the server share")

// proxyLink serves the players of proxies on l and returns a link a proxy
// dialed to it.
func proxyLink(t *testing.T, l *Listener) *proxy.Link {
	t.Helper()
	pl, err := proxy.Listen("127.0.0.1:0", proxySecret)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pl.Close() })
	go func() { _ = l.ServeProxy(pl) }()
	link, err := proxy.Dial(pl.Addr().String(), proxySecret)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = link.Close() })
	return link
}

// openProxied opens a player of the name passed on the link, as a proxy
// that logged it in does, and returns the batches of the player.
func openProxied(t *testing.T, link *proxy.Link, name string, protocol int32) *bedrock.Conn {
	t.Helper()
	return openProxiedAs(t, link, name, protocol, true)
}

// openProxiedAs is openProxied of a player whose proxy says Xbox Live
// vouched for it if xboxLive.
func openProxiedAs(t *testing.T, link *proxy.Link, name string, protocol int32, xboxLive bool) *bedrock.Conn {
	t.Helper()
	identity, err := json.Marshal(ourlogin.Identity{DisplayName: name, Identity: uuid.NewString(), XUID: "2535400000000000"})
	if err != nil {
		t.Fatal(err)
	}
	client, err := json.Marshal(ourlogin.ClientData{GameVersion: mc.Version, LanguageCode: "ru_RU", ServerAddress: "play.example.org:19132"})
	if err != nil {
		t.Fatal(err)
	}
	p, err := link.Open(proxy.Token{Identity: identity, ClientData: client, XboxLive: xboxLive, Addr: "203.0.113.7:4242", Protocol: protocol})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	_ = p.SetReadDeadline(time.Now().Add(10 * time.Second))
	b := bedrock.NewConn(p, bedrock.Config{})
	b.EnableCompression(bedrock.None{})
	return b
}

// readUntil reads the packets of the server until one of the ID passed,
// which it decodes as a client does.
func readUntil(t *testing.T, b *bedrock.Conn, id uint32) proto.Packet {
	t.Helper()
	for {
		raw, err := b.ReadPacket()
		if err != nil {
			t.Fatalf("waiting for packet %d: %v", id, err)
		}
		h, fields, err := bedrock.ReadHeader(raw)
		if err != nil {
			t.Fatal(err)
		}
		if h.ID != id {
			continue
		}
		pk := mc.New(id)
		r := wire.NewReader(fields)
		pk.Decode(r)
		if r.Err() != nil {
			t.Fatal(r.Err())
		}
		return pk
	}
}

// TestProxiedPlayerJoins has a proxy open a player on a link: the server
// takes who it is and where it came from out of the token, hands it the
// packs, spawns it, and packets go both ways over the link uncompressed.
func TestProxiedPlayerJoins(t *testing.T) {
	l := listen(t, Config{})
	spawned := serve(t, l)
	b := openProxied(t, proxyLink(t, l), "Proxied", mc.Protocol)

	if st := readUntil(t, b, mc.IDPlayStatus).(*mc.PlayStatusPacket); st.Status != mc.PlayStatusLoginSuccess {
		t.Fatalf("status %d, want the login done", st.Status)
	}
	readUntil(t, b, mc.IDResourcePacksInfo)
	writeRaw(t, b, &mc.ResourcePackClientResponsePacket{Response: mc.PackResponseAllPacksDownloaded})
	readUntil(t, b, mc.IDResourcePackStack)
	writeRaw(t, b, &mc.ResourcePackClientResponsePacket{Response: mc.PackResponseCompleted})
	game := readUntil(t, b, mc.IDStartGame).(*mc.StartGamePacket)
	writeRaw(t, b, &mc.RequestChunkRadiusPacket{ChunkRadius: 8})
	if st := readUntil(t, b, mc.IDPlayStatus).(*mc.PlayStatusPacket); st.Status != mc.PlayStatusPlayerSpawn {
		t.Fatalf("status %d, want the spawn", st.Status)
	}
	writeRaw(t, b, &mc.SetLocalPlayerAsInitialisedPacket{EntityRuntimeID: game.EntityRuntimeID})

	var c *Conn
	select {
	case c = <-spawned:
	case <-time.After(10 * time.Second):
		t.Fatal("the proxied player did not spawn")
	}
	if id := c.IdentityData(); id.DisplayName != "Proxied" || id.XUID != "2535400000000000" {
		t.Fatalf("identity %+v", id)
	}
	if !c.Authenticated() {
		t.Fatal("a player whose proxy says Xbox Live vouched for it is not authenticated")
	}
	if cd := c.ClientData(); cd.LanguageCode != "ru_RU" || cd.ServerAddress != "play.example.org:19132" {
		t.Fatalf("client data %+v", cd)
	}
	if addr := c.RemoteAddr().String(); addr != "203.0.113.7:4242" {
		t.Fatalf("address %s, want the one the proxy saw", addr)
	}
	if st := c.Stats(); !st.Proxied || st.Remote.String() != "203.0.113.7:4242" {
		t.Fatalf("stats %+v", st)
	}
	writeRaw(t, b, &mc.TextPacket{TextType: mc.TextTypeChat, SourceName: "Proxied", Message: "hi"})
	if echo := readUntil(t, b, mc.IDText).(*mc.TextPacket); echo.Message != "echo hi" {
		t.Fatalf("echo %q", echo.Message)
	}
}

// TestProxiedPlayersAdmittedAsClients checks a proxied player passes the
// rules of the server as a client of its own does: its protocol, Validate
// and Allow.
func TestProxiedPlayersAdmittedAsClients(t *testing.T) {
	refused := func(identity ourlogin.Identity, client ourlogin.ClientData) error {
		if identity.DisplayName == "Invalid" {
			return errInvalidName
		}
		return nil
	}
	l := listen(t, Config{Validate: refused})
	link := proxyLink(t, l)
	if st := readUntil(t, openProxied(t, link, "Old", 1), mc.IDPlayStatus).(*mc.PlayStatusPacket); st.Status != mc.PlayStatusLoginFailedClient {
		t.Fatalf("status %d for an old protocol", st.Status)
	}
	b := openProxied(t, link, "Invalid", mc.Protocol)
	if _, err := b.ReadPacket(); err == nil {
		t.Fatal("a player the rules refuse was let in")
	}
}

// TestProxiedProtocols checks the protocol of a proxied player is one the
// listener allows, refused as outdated otherwise, newer or older, as it
// was before the listener allowed more than one.
func TestProxiedProtocols(t *testing.T) {
	l := listen(t, Config{Protocols: []*versions.Protocol{versions.Model(), fakeProtocol}})
	link := proxyLink(t, l)
	for _, n := range []int32{mc.Protocol - 1, 2250, fakeProtocol.Number + 1} {
		if st := readUntil(t, openProxied(t, link, "Refused", n), mc.IDPlayStatus).(*mc.PlayStatusPacket); st.Status != mc.PlayStatusLoginFailedClient {
			t.Errorf("status %d for protocol %d", st.Status, n)
		}
	}
	accepted := make(chan *Conn, 1)
	go func() {
		if c, err := l.Accept(); err == nil {
			accepted <- c
		}
	}()
	b := openProxied(t, link, "Newer", fakeProtocol.Number)
	if st := readUntil(t, b, mc.IDPlayStatus).(*mc.PlayStatusPacket); st.Status != mc.PlayStatusLoginSuccess {
		t.Fatalf("status %d for protocol %d", st.Status, fakeProtocol.Number)
	}
	// The client is told the version of its own protocol.
	writeRaw(t, b, &mc.ResourcePackClientResponsePacket{Response: mc.PackResponseAllPacksDownloaded})
	if stack := readUntil(t, b, mc.IDResourcePackStack).(*mc.ResourcePackStackPacket); stack.BaseGameVersion != fakeProtocol.Version {
		t.Errorf("the stack of packs is of %s, not of %s", stack.BaseGameVersion, fakeProtocol.Version)
	}
	writeRaw(t, b, &mc.ResourcePackClientResponsePacket{Response: mc.PackResponseCompleted})
	var c *Conn
	select {
	case c = <-accepted:
		if c.Protocol() != fakeProtocol || c.Stats().Protocol != fakeProtocol.Number {
			t.Fatalf("protocol %v, stats %d", c.Protocol(), c.Stats().Protocol)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the proxied player was not accepted")
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = c.StartGameContext(ctx, gameData())
	}()
	if game := readUntil(t, b, mc.IDStartGame).(*mc.StartGamePacket); game.GameVersion != fakeProtocol.Version || game.BaseGameVersion != mc.Version {
		t.Errorf("StartGame of %s on %s, want the game of %s on what the session says", game.GameVersion, game.BaseGameVersion, fakeProtocol.Version)
	}
	if l.Advertised() != fakeProtocol {
		t.Errorf("the listener advertises %v, not the newest protocol it allows", l.Advertised())
	}
	if p := listen(t, Config{}).Advertised(); p != versions.Model() {
		t.Errorf("a listener of the model alone advertises %v", p)
	}
}

var errInvalidName = wire.ErrTooLong

// TestProxiedPlayerAuthenticatedAsItsTokenSays checks that Allow is told
// whether Xbox Live vouched for a proxied player as the token of its proxy
// says, and nothing else.
func TestProxiedPlayerAuthenticatedAsItsTokenSays(t *testing.T) {
	told := make(chan bool, 2)
	l := listen(t, Config{Allow: func(_ net.Addr, id ourlogin.Identity, _ ourlogin.ClientData, xboxLive bool) (string, bool) {
		told <- xboxLive
		return "refused", false
	}})
	link := proxyLink(t, l)
	for _, xboxLive := range []bool{true, false} {
		b := openProxiedAs(t, link, "Proxied", mc.Protocol, xboxLive)
		if _, err := b.ReadPacket(); err != nil {
			t.Fatal(err)
		}
		select {
		case got := <-told:
			if got != xboxLive {
				t.Errorf("Allow was told %t of a token that says %t", got, xboxLive)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("Allow was not called")
		}
	}
}

// TestProxiedPlayerReadsStatics sends a Static to a proxied player and to
// a client of the server's own: each reads it in a batch of the
// compression of its connection, uncompressed over the link, and the
// session goes on after it.
func TestProxiedPlayerReadsStatics(t *testing.T) {
	l := listen(t, Config{})
	st := &staticText{&mc.TextPacket{TextType: mc.TextTypeRaw, Message: strings.Repeat("static ", 100)}}
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				if err := c.StartGameContext(ctx, gameData()); err != nil {
					_ = c.Close()
					return
				}
				_ = c.WritePacket(st)
				_ = c.WritePacket(&mc.TextPacket{TextType: mc.TextTypeRaw, Message: "after"})
				_ = c.Flush()
			}()
		}
	}()

	b := openProxied(t, proxyLink(t, l), "Proxied", mc.Protocol)
	readUntil(t, b, mc.IDResourcePacksInfo)
	writeRaw(t, b, &mc.ResourcePackClientResponsePacket{Response: mc.PackResponseAllPacksDownloaded})
	readUntil(t, b, mc.IDResourcePackStack)
	writeRaw(t, b, &mc.ResourcePackClientResponsePacket{Response: mc.PackResponseCompleted})
	game := readUntil(t, b, mc.IDStartGame).(*mc.StartGamePacket)
	writeRaw(t, b, &mc.RequestChunkRadiusPacket{ChunkRadius: 8})
	readUntil(t, b, mc.IDPlayStatus)
	writeRaw(t, b, &mc.SetLocalPlayerAsInitialisedPacket{EntityRuntimeID: game.EntityRuntimeID})
	if text := readUntil(t, b, mc.IDText).(*mc.TextPacket); text.Message != st.Packet.(*mc.TextPacket).Message {
		t.Fatalf("read %q, want the static", text.Message)
	}
	if text := readUntil(t, b, mc.IDText).(*mc.TextPacket); text.Message != "after" {
		t.Fatalf("read %q after the static", text.Message)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	conn, err := ourclient.Dial(ctx, l.Addr().String(), ourclient.Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	var got []string
	for len(got) < 2 {
		pk, err := conn.ReadPacket()
		if err != nil {
			t.Fatal(err)
		}
		if text, ok := pk.(*mc.TextPacket); ok {
			got = append(got, text.Message)
		}
	}
	if got[0] != st.Packet.(*mc.TextPacket).Message || got[1] != "after" {
		t.Fatalf("the direct client read %q", got)
	}

	l.staticMu.Lock()
	defer l.staticMu.Unlock()
	if len(l.statics) != 2 {
		t.Fatalf("%d static batches, want one of each compression", len(l.statics))
	}
	for key, msg := range l.statics {
		w := wire.Writer{B: bedrock.AppendHeader(nil, bedrock.Header{ID: st.ID()})}
		st.Encode(&w)
		comp := bedrock.Compression(bedrock.None{})
		if key.alg != bedrock.AlgorithmNone {
			comp = staticCompression
		}
		want, err := bedrock.EncodeBatch(comp, w.B)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(msg, want) {
			t.Fatalf("the batch of algorithm %#x is not the static encoded in it", key.alg)
		}
	}
}
