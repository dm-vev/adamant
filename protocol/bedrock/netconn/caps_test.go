package netconn

import (
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/dm-vev/adamant/net/bedrock"
	ourlogin "github.com/dm-vev/adamant/net/bedrock/login"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/dm-vev/adamant/net/proxy"
	"github.com/google/uuid"
)

// openProxiedFrom opens a player on the link as a proxy that saw it at
// addr does, and returns its batches.
func openProxiedFrom(t *testing.T, link *proxy.Link, name, addr string) *bedrock.Conn {
	t.Helper()
	identity, _ := json.Marshal(ourlogin.Identity{DisplayName: name, Identity: uuid.NewString(), XUID: "2535400000000000"})
	client, _ := json.Marshal(ourlogin.ClientData{GameVersion: mc.Version, LanguageCode: "en_US"})
	p, err := link.Open(proxy.Token{Identity: identity, ClientData: client, XboxLive: true, Addr: addr, Protocol: mc.Protocol})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	_ = p.SetReadDeadline(time.Now().Add(10 * time.Second))
	b := bedrock.NewConn(p, bedrock.Config{})
	b.EnableCompression(bedrock.None{})
	return b
}

// spawnProxied takes a proxied player from its login to its spawn.
func spawnProxied(t *testing.T, b *bedrock.Conn) {
	t.Helper()
	if st := readUntil(t, b, mc.IDPlayStatus).(*mc.PlayStatusPacket); st.Status != mc.PlayStatusLoginSuccess {
		t.Fatalf("status %d, want the login done", st.Status)
	}
	readUntil(t, b, mc.IDResourcePacksInfo)
	writeRaw(t, b, &mc.ResourcePackClientResponsePacket{Response: mc.PackResponseAllPacksDownloaded})
	readUntil(t, b, mc.IDResourcePackStack)
	writeRaw(t, b, &mc.ResourcePackClientResponsePacket{Response: mc.PackResponseCompleted})
	game := readUntil(t, b, mc.IDStartGame).(*mc.StartGamePacket)
	writeRaw(t, b, &mc.RequestChunkRadiusPacket{ChunkRadius: 8})
	readUntil(t, b, mc.IDPlayStatus)
	writeRaw(t, b, &mc.SetLocalPlayerAsInitialisedPacket{EntityRuntimeID: game.EntityRuntimeID})
}

// refusedForAddress reads the packets of a client until the Disconnect
// that refuses it for its address.
func refusedForAddress(t *testing.T, b *bedrock.Conn) {
	t.Helper()
	if d := readUntil(t, b, mc.IDDisconnect).(*mc.DisconnectPacket); d.Message != errAddressBusy {
		t.Fatalf("disconnect %q, want %q", d.Message, errAddressBusy)
	}
}

// TestMaxPerAddressRefusesLogins checks that the clients of one address
// logging in at once are bounded, and no other address is.
func TestMaxPerAddressRefusesLogins(t *testing.T) {
	l := listen(t, Config{MaxPerAddress: 2})
	link := proxyLink(t, l)
	for _, name := range []string{"One", "Two"} {
		// Each stops at the resource packs, logging in.
		readUntil(t, openProxiedFrom(t, link, name, "203.0.113.7:4242"), mc.IDResourcePacksInfo)
	}
	refusedForAddress(t, openProxiedFrom(t, link, "Three", "203.0.113.7:4343"))
	readUntil(t, openProxiedFrom(t, link, "Other", "198.51.100.1:4242"), mc.IDResourcePacksInfo)
}

// TestMaxPerAddressHoldsPlayers checks that a client that joined still
// counts against its address, an IPv6 /64 as one, until it leaves.
func TestMaxPerAddressHoldsPlayers(t *testing.T) {
	l := listen(t, Config{MaxPerAddress: 1})
	spawned := serve(t, l)
	link := proxyLink(t, l)
	spawnProxied(t, openProxiedFrom(t, link, "First", "[2001:db8:1:2::7]:4242"))
	var c *Conn
	select {
	case c = <-spawned:
	case <-time.After(10 * time.Second):
		t.Fatal("the first player did not spawn")
	}
	refusedForAddress(t, openProxiedFrom(t, link, "Second", "[2001:db8:1:2::8]:4242"))

	_ = c.Close()
	readUntil(t, openProxiedFrom(t, link, "Third", "[2001:db8:1:2::9]:4242"), mc.IDResourcePacksInfo)
}

// TestMaxLoginsRefusesLogins checks that the clients logging in at once are
// bounded, loopback ones too, and that one is counted out once its login
// ends.
func TestMaxLoginsRefusesLogins(t *testing.T) {
	l := listen(t, Config{MaxLogins: 1})
	first := rawClient(t, l)
	waitLogins(t, l, 1)
	if got := readStatus(t, rawClient(t, l)); got != mc.PlayStatusLoginFailedServerFull {
		t.Fatalf("status %d for a login over MaxLogins", got)
	}
	writeRaw(t, first, &mc.RequestNetworkSettingsPacket{ClientProtocol: 1})
	readStatus(t, first)
	waitLogins(t, l, 0)
	b := rawClient(t, l)
	writeRaw(t, b, &mc.RequestNetworkSettingsPacket{ClientProtocol: 1})
	if got := readStatus(t, b); got != mc.PlayStatusLoginFailedClient {
		t.Fatalf("status %d for a login after the first ended", got)
	}
}

// waitLogins waits for the listener to count n clients logging in.
func waitLogins(t *testing.T, l *Listener, n int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		l.admitMu.Lock()
		got := l.logins
		l.admitMu.Unlock()
		if got == n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d clients logging in, want %d", got, n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestAddressSource(t *testing.T) {
	for addr, want := range map[net.Addr]string{
		&net.UDPAddr{IP: net.IPv4(203, 0, 113, 7), Port: 1}:            "203.0.113.7/32",
		&net.UDPAddr{IP: net.ParseIP("2001:db8:1:2:3:4:5:6"), Port: 1}: "2001:db8:1:2::/64",
		&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}:              "invalid Prefix",
		&net.UDPAddr{IP: net.IPv6loopback, Port: 1}:                    "invalid Prefix",
		&net.UDPAddr{}: "invalid Prefix",
	} {
		if got := addressSource(addr).String(); got != want {
			t.Errorf("source of %v = %s, want %s", addr, got, want)
		}
	}
}

// TestServerFullRefusesProxiedPlayer checks that a player of a proxy reads
// the refusal of a full server in the batches of its link, uncompressed.
func TestServerFullRefusesProxiedPlayer(t *testing.T) {
	l := listen(t, Config{MaximumPlayers: 1})
	link := proxyLink(t, l)
	readUntil(t, openProxiedFrom(t, link, "One", "203.0.113.7:4242"), mc.IDResourcePacksInfo)
	st := readUntil(t, openProxiedFrom(t, link, "Two", "198.51.100.1:4242"), mc.IDPlayStatus).(*mc.PlayStatusPacket)
	if st.Status != mc.PlayStatusLoginFailedServerFull {
		t.Fatalf("status %d, want the server full", st.Status)
	}
}
