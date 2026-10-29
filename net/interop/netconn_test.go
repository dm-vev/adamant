package interop

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/netconn"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/dm-vev/adamant/net/bedrock/resource"
	"github.com/dm-vev/adamant/net/nethernet"
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft"
	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// gtNetwork dials gophertunnel clients over our NetherNet dialer.
type gtNetwork struct{ d nethernet.Dialer }

func (n gtNetwork) DialContext(ctx context.Context, address string) (net.Conn, error) {
	c, err := n.d.DialContext(ctx, address)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (gtNetwork) PingContext(context.Context, string) ([]byte, error) {
	return nil, errors.New("no ping")
}

func (gtNetwork) Listen(string) (minecraft.NetworkListener, error) {
	return nil, errors.New("no listen")
}

// boundProtocol makes the Login of gophertunnel carry the key of the
// transport identity, as the game's Login does.
type boundProtocol struct {
	minecraft.Protocol
	key *ecdsa.PrivateKey
}

func (p boundProtocol) ConvertFromLatest(pk packet.Packet, conn *minecraft.Conn) []packet.Packet {
	if l, ok := pk.(*packet.Login); ok {
		l.ConnectionRequest = login.EncodeOffline(conn.IdentityData(), conn.ClientData(), p.key, false)
	}
	return []packet.Packet{pk}
}

// gtClient returns a gophertunnel dialer and the network it dials over,
// whose transport identity and Login carry one key unless unbound.
func gtClient(t *testing.T, name string, unbound bool) (minecraft.Dialer, gtNetwork) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	identity := login.IdentityData{DisplayName: name, Identity: uuid.NewString()}
	request := login.EncodeOffline(identity, login.ClientData{}, key, false)
	var cert struct{ Token string }
	n := binary.LittleEndian.Uint32(request[:4])
	if err := json.Unmarshal(request[4:4+n], &cert); err != nil {
		t.Fatal(err)
	}
	d := minecraft.Dialer{IdentityData: identity}
	if !unbound {
		d.Protocol = boundProtocol{Protocol: minecraft.DefaultProtocol, key: key}
	}
	return d, gtNetwork{nethernet.Dialer{Key: key, Token: cert.Token}}
}

func netconnListen(t *testing.T, cfg netconn.Config) *netconn.Listener {
	t.Helper()
	cfg.AuthenticationDisabled = true
	l, err := cfg.Listen(nethernet.ListenConfig{}, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	return l
}

func netconnGameData() netconn.GameData {
	return netconn.GameData{
		WorldName:       "netconn",
		EntityUniqueID:  1,
		EntityRuntimeID: 1,
		PlayerPosition:  [3]float32{0, 64, 0},
		BaseGameVersion: v2193.Version,
		Items:           []v2193.ItemEntry{{Name: "minecraft:shield", RuntimeID: 355}},
	}
}

// netconnServe accepts one client, spawns it, writes two texts of the
// generated codecs and answers each Text with an echo.
func netconnServe(t *testing.T, l *netconn.Listener) <-chan *netconn.Conn {
	spawned := make(chan *netconn.Conn, 1)
	go func() {
		c, err := l.Accept()
		if err != nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := c.StartGameContext(ctx, netconnGameData()); err != nil {
			t.Errorf("start game: %v", err)
			_ = c.Close()
			return
		}
		_ = c.WritePacket(&v2193.TextPacket{TextType: v2193.TextTypeRaw, Message: "first"})
		_ = c.WritePacket(&v2193.TextPacket{TextType: v2193.TextTypeRaw, Message: "generated"})
		_ = c.Flush()
		spawned <- c
		for {
			pk, err := c.ReadPacket()
			if err != nil {
				return
			}
			if text, ok := pk.(*v2193.TextPacket); ok {
				_ = c.WritePacket(&v2193.TextPacket{TextType: v2193.TextTypeRaw, Message: "echo " + text.Message})
				_ = c.Flush()
			}
		}
	}()
	return spawned
}

func readGTText(t *testing.T, conn *minecraft.Conn) string {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	for {
		pk, err := conn.ReadPacket()
		if err != nil {
			t.Fatal(err)
		}
		if text, ok := pk.(*packet.Text); ok {
			return text.Message
		}
	}
}

// TestGophertunnelClientJoinsNetconn logs a gophertunnel client in to the
// login machine of the server, spawns it, has it read packets of the
// generated codecs in the order they were written and exchanges packets
// after, until the server disconnects it.
func TestGophertunnelClientJoinsNetconn(t *testing.T) {
	l := netconnListen(t, netconn.Config{})
	spawned := netconnServe(t, l)
	d, n := gtClient(t, "Bot", false)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	conn, err := d.DialContextNetwork(ctx, n, l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.DoSpawnContext(ctx); err != nil {
		t.Fatal(err)
	}
	var srv *netconn.Conn
	select {
	case srv = <-spawned:
	case <-ctx.Done():
		t.Fatal("server did not spawn the client")
	}
	if got := conn.GameData().WorldName; got != "netconn" {
		t.Fatalf("world name %q", got)
	}
	if srv.IdentityData().DisplayName != "Bot" || srv.ChunkRadius() < 1 {
		t.Fatalf("server sees %q with chunk radius %d", srv.IdentityData().DisplayName, srv.ChunkRadius())
	}
	for _, want := range []string{"first", "generated"} {
		if got := readGTText(t, conn); got != want {
			t.Fatalf("read %q, want %q", got, want)
		}
	}
	for _, msg := range []string{"hello", strings.Repeat("long ", 300)} {
		if err := conn.WritePacket(&packet.Text{TextType: packet.TextTypeChat, SourceName: "Bot", Message: msg}); err != nil {
			t.Fatal(err)
		}
		if got := readGTText(t, conn); got != "echo "+msg {
			t.Fatalf("echo %q", got)
		}
	}
	if err := l.Disconnect(srv, "bye"); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	for {
		if _, err := conn.ReadPacket(); err != nil {
			if !strings.Contains(err.Error(), "bye") {
				t.Fatalf("read after the disconnect: %v", err)
			}
			break
		}
	}
}

// TestNetconnRefusesUnboundLogin refuses a gophertunnel Login whose key is
// not the one the transport identity proved: a login replayed on another
// connection.
func TestNetconnRefusesUnboundLogin(t *testing.T) {
	l := netconnListen(t, netconn.Config{})
	netconnServe(t, l)
	d, n := gtClient(t, "Replay", true)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	conn, err := d.DialContextNetwork(ctx, n, l.Addr().String())
	if err == nil {
		_ = conn.Close()
		t.Fatal("joined with a login key the transport did not prove")
	}
	var refused minecraft.DisconnectError
	if !errors.As(err, &refused) {
		t.Fatalf("dial failed without a disconnect: %v", err)
	}
}

// testPack returns a resource pack of size bytes of textures, more than a
// chunk of the packs netconn sends.
func testPack(t *testing.T, size int) *resource.Pack {
	t.Helper()
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	manifest, _ := z.Create("manifest.json")
	fmt.Fprintf(manifest, `{"format_version":2,"header":{"name":"test","description":"","uuid":%q,"version":[1,0,0],"min_engine_version":[1,20,0]},"modules":[{"type":"resources","uuid":%q,"version":[1,0,0]}]}`, uuid.NewString(), uuid.NewString())
	texture, _ := z.Create("textures/blob.bin")
	blob := make([]byte, size)
	_, _ = rand.Read(blob)
	_, _ = texture.Write(blob)
	_ = z.Close()
	pack, err := resource.Read(&buf)
	if err != nil {
		t.Fatal(err)
	}
	return pack
}

// TestNetconnResourcePacks has a gophertunnel client download two packs
// of several chunks each.
func TestNetconnResourcePacks(t *testing.T) {
	packs := []*resource.Pack{testPack(t, 300<<10), testPack(t, 10<<10)}
	l := netconnListen(t, netconn.Config{ResourcePacks: packs, TexturePacksRequired: true})
	spawned := netconnServe(t, l)
	d, n := gtClient(t, "Packs", false)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	conn, err := d.DialContextNetwork(ctx, n, l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	got := conn.ResourcePacks()
	if len(got) != len(packs) {
		t.Fatalf("client has %d packs, want %d", len(got), len(packs))
	}
	for _, want := range packs {
		found := false
		for _, p := range got {
			found = found || p.UUID().String() == want.UUID().String() && p.Checksum() == want.Checksum()
		}
		if !found {
			t.Fatalf("pack %v missing or corrupt", want.UUID())
		}
	}
	// The client is done once it dialed, the server once it read that the
	// client spawned: closing before would fail the spawn after the test.
	select {
	case <-spawned:
	case <-ctx.Done():
		t.Fatal("server did not spawn the client")
	}
}
