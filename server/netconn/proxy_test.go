package netconn

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/dm-vev/adamant/net/bedrock"
	ourlogin "github.com/dm-vev/adamant/net/bedrock/login"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
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
	identity, err := json.Marshal(ourlogin.Identity{DisplayName: name, Identity: uuid.NewString(), XUID: "2535400000000000"})
	if err != nil {
		t.Fatal(err)
	}
	client, err := json.Marshal(ourlogin.ClientData{GameVersion: v2193.Version, LanguageCode: "ru_RU", ServerAddress: "play.example.org:19132"})
	if err != nil {
		t.Fatal(err)
	}
	p, err := link.Open(proxy.Token{Identity: identity, ClientData: client, XboxLive: true, Addr: "203.0.113.7:4242", Protocol: protocol})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	_ = p.SetReadDeadline(time.Now().Add(10 * time.Second))
	b := bedrock.NewConn(p, bedrock.Config{})
	b.EnableCompression(bedrock.None{})
	return b
}

// readUntil reads the packets of the server until one of the ID passed.
func readUntil(t *testing.T, b *bedrock.Conn, id uint32) proto.Packet {
	t.Helper()
	for {
		raw, err := b.ReadPacket()
		if err != nil {
			t.Fatalf("waiting for packet %d: %v", id, err)
		}
		h, _, err := bedrock.ReadHeader(raw)
		if err != nil {
			t.Fatal(err)
		}
		if h.ID != id {
			continue
		}
		pk, err := DecodePacket(raw)
		if err != nil {
			t.Fatal(err)
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
	b := openProxied(t, proxyLink(t, l), "Proxied", v2193.Protocol)

	if st := readUntil(t, b, v2193.IDPlayStatus).(*v2193.PlayStatusPacket); st.Status != v2193.PlayStatusLoginSuccess {
		t.Fatalf("status %d, want the login done", st.Status)
	}
	readUntil(t, b, v2193.IDResourcePacksInfo)
	writeRaw(t, b, &v2193.ResourcePackClientResponsePacket{Response: v2193.PackResponseAllPacksDownloaded})
	readUntil(t, b, v2193.IDResourcePackStack)
	writeRaw(t, b, &v2193.ResourcePackClientResponsePacket{Response: v2193.PackResponseCompleted})
	game := readUntil(t, b, v2193.IDStartGame).(*v2193.StartGamePacket)
	writeRaw(t, b, &v2193.RequestChunkRadiusPacket{ChunkRadius: 8})
	if st := readUntil(t, b, v2193.IDPlayStatus).(*v2193.PlayStatusPacket); st.Status != v2193.PlayStatusPlayerSpawn {
		t.Fatalf("status %d, want the spawn", st.Status)
	}
	writeRaw(t, b, &v2193.SetLocalPlayerAsInitialisedPacket{EntityRuntimeID: game.EntityRuntimeID})

	var c *Conn
	select {
	case c = <-spawned:
	case <-time.After(10 * time.Second):
		t.Fatal("the proxied player did not spawn")
	}
	if id := c.IdentityData(); id.DisplayName != "Proxied" || id.XUID != "2535400000000000" {
		t.Fatalf("identity %+v", id)
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
	writeRaw(t, b, &v2193.TextPacket{TextType: v2193.TextTypeChat, SourceName: "Proxied", Message: "hi"})
	if echo := readUntil(t, b, v2193.IDText).(*v2193.TextPacket); echo.Message != "echo hi" {
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
	if st := readUntil(t, openProxied(t, link, "Old", 1), v2193.IDPlayStatus).(*v2193.PlayStatusPacket); st.Status != v2193.PlayStatusLoginFailedClient {
		t.Fatalf("status %d for an old protocol", st.Status)
	}
	b := openProxied(t, link, "Invalid", v2193.Protocol)
	if _, err := b.ReadPacket(); err == nil {
		t.Fatal("a player the rules refuse was let in")
	}
}

var errInvalidName = wire.ErrTooLong
