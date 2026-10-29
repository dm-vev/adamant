package netconn

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

	"github.com/dm-vev/adamant/net/bedrock"
	ourclient "github.com/dm-vev/adamant/net/bedrock/client"
	ourlogin "github.com/dm-vev/adamant/net/bedrock/login"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/dm-vev/adamant/net/nethernet"
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	"github.com/sandertv/gophertunnel/minecraft/resource"
)

// network dials gophertunnel clients over our NetherNet dialer.
type network struct{ d nethernet.Dialer }

func (n network) DialContext(ctx context.Context, address string) (net.Conn, error) {
	c, err := n.d.DialContext(ctx, address)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (network) PingContext(context.Context, string) ([]byte, error) {
	return nil, errors.New("no ping")
}

func (network) Listen(string) (minecraft.NetworkListener, error) {
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

// client returns a gophertunnel dialer and the network it dials over,
// whose transport identity and Login carry one key unless unbound.
func client(t *testing.T, name string, unbound bool) (minecraft.Dialer, network) {
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
	return d, network{nethernet.Dialer{Key: key, Token: cert.Token}}
}

func listen(t *testing.T, cfg Config) *Listener {
	t.Helper()
	cfg.AuthenticationDisabled = true
	l, err := cfg.Listen(nethernet.ListenConfig{}, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	return l
}

func gameData() minecraft.GameData {
	return minecraft.GameData{
		WorldName:       "netconn",
		EntityUniqueID:  1,
		EntityRuntimeID: 1,
		PlayerPosition:  [3]float32{0, 64, 0},
		BaseGameVersion: protocol.CurrentVersion,
		Items:           []protocol.ItemEntry{{Name: "minecraft:shield", RuntimeID: 355}},
	}
}

// serve accepts one client, spawns it and answers each Text with an echo.
func serve(t *testing.T, l *Listener) <-chan *Conn {
	spawned := make(chan *Conn, 1)
	go func() {
		c, err := l.Accept()
		if err != nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := c.StartGameContext(ctx, gameData()); err != nil {
			t.Errorf("start game: %v", err)
			_ = c.Close()
			return
		}
		spawned <- c
		for {
			pk, err := c.ReadPacket()
			if err != nil {
				return
			}
			if text, ok := pk.(*packet.Text); ok {
				_ = c.WritePacket(&packet.Text{TextType: packet.TextTypeRaw, Message: "echo " + text.Message})
				_ = c.Flush()
			}
		}
	}()
	return spawned
}

func readText(t *testing.T, conn *minecraft.Conn) string {
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

// TestLoginAndSpawn logs a gophertunnel client in, spawns it and exchanges
// packets over the connection afterwards.
func TestLoginAndSpawn(t *testing.T) {
	l := listen(t, Config{})
	spawned := serve(t, l)
	d, n := client(t, "Bot", false)
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
	var srv *Conn
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
	for _, msg := range []string{"hello", strings.Repeat("long ", 300)} {
		if err := conn.WritePacket(&packet.Text{TextType: packet.TextTypeChat, SourceName: "Bot", Message: msg}); err != nil {
			t.Fatal(err)
		}
		if got := readText(t, conn); got != "echo "+msg {
			t.Fatalf("echo %q", got)
		}
	}
	if err := l.Disconnect(srv, "bye"); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	for {
		_, err := conn.ReadPacket()
		if err != nil {
			if !strings.Contains(err.Error(), "bye") {
				t.Fatalf("read after the disconnect: %v", err)
			}
			break
		}
	}
}

// TestLoginKeyMustMatchTransport refuses a Login whose key is not the one
// the transport identity proved: a login replayed on another connection.
func TestLoginKeyMustMatchTransport(t *testing.T) {
	l := listen(t, Config{})
	serve(t, l)
	d, n := client(t, "Replay", true)
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

// rawClient opens a NetherNet connection that speaks batches directly.
func rawClient(t *testing.T, l *Listener) *bedrock.Conn {
	t.Helper()
	d, n := client(t, "Raw", false)
	_ = d
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	nn, err := n.d.DialContext(ctx, l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = nn.Close() })
	_ = nn.SetReadDeadline(time.Now().Add(10 * time.Second))
	return bedrock.NewConn(nn, bedrock.Config{})
}

func writeRaw(t *testing.T, b *bedrock.Conn, pk packet.Packet) {
	t.Helper()
	buf := bytes.NewBuffer(bedrock.AppendHeader(nil, bedrock.Header{ID: pk.ID()}))
	pk.Marshal(protocol.NewWriter(buf, 0))
	if err := b.WritePacket(buf.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := b.Flush(); err != nil {
		t.Fatal(err)
	}
}

func readStatus(t *testing.T, b *bedrock.Conn) int32 {
	t.Helper()
	raw, err := b.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	h, fields, err := bedrock.ReadHeader(raw)
	if err != nil || h.ID != packet.IDPlayStatus {
		t.Fatalf("packet %d, %v", h.ID, err)
	}
	var pk packet.PlayStatus
	pk.Marshal(protocol.NewReader(bytes.NewBuffer(fields), 0, false))
	return pk.Status
}

func TestProtocolMismatch(t *testing.T) {
	l := listen(t, Config{})
	for proto, want := range map[int32]int32{1: packet.PlayStatusLoginFailedClient, protocol.CurrentProtocol + 1: packet.PlayStatusLoginFailedServer} {
		b := rawClient(t, l)
		writeRaw(t, b, &packet.RequestNetworkSettings{ClientProtocol: proto})
		if got := readStatus(t, b); got != want {
			t.Fatalf("protocol %d: status %d, want %d", proto, got, want)
		}
	}
}

func TestServerFull(t *testing.T) {
	l := listen(t, Config{MaximumPlayers: 1})
	serve(t, l)
	d, n := client(t, "First", false)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	conn, err := d.DialContextNetwork(ctx, n, l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	b := rawClient(t, l)
	if got := readStatus(t, b); got != packet.PlayStatusLoginFailedServerFull {
		t.Fatalf("status %d for the second client", got)
	}
}

// testPack returns a resource pack of size bytes of textures, more than a
// chunk of packChunkSize.
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

// TestResourcePacks downloads two packs of several chunks each.
func TestResourcePacks(t *testing.T) {
	packs := []*resource.Pack{testPack(t, 300<<10), testPack(t, 10<<10)}
	l := listen(t, Config{ResourcePacks: packs, TexturePacksRequired: true})
	serve(t, l)
	d, n := client(t, "Packs", false)
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
			found = found || p.UUID() == want.UUID() && p.Checksum() == want.Checksum()
		}
		if !found {
			t.Fatalf("pack %v missing or corrupt", want.UUID())
		}
	}
}

// TestOurClientJoins logs in and spawns with the client of the net module,
// which codes packets with the generated codecs, and exchanges packets
// after. It dials a signaling URL, which the client data must not name.
func TestOurClientJoins(t *testing.T) {
	l := listen(t, Config{})
	spawned := serve(t, l)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	conn, err := ourclient.Dial(ctx, "http://"+l.Addr().String(), ourclient.Config{Identity: ourlogin.Identity{DisplayName: "Bot"}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var srv *Conn
	select {
	case srv = <-spawned:
	case <-ctx.Done():
		t.Fatal("server did not spawn the client")
	}
	if got := conn.StartGame().WorldName; got != "netconn" {
		t.Fatalf("world name %q", got)
	}
	if srv.IdentityData().DisplayName != "Bot" || srv.ChunkRadius() != 8 {
		t.Fatalf("server sees %q with chunk radius %d", srv.IdentityData().DisplayName, srv.ChunkRadius())
	}
	if err := conn.WritePacket(&v2193.TextPacket{TextType: 1, SourceName: "Bot", Message: "hello"}); err != nil {
		t.Fatal(err)
	}
	if err := conn.Flush(); err != nil {
		t.Fatal(err)
	}
	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()
	for {
		pk, err := conn.ReadPacket()
		if err != nil {
			t.Fatal(err)
		}
		if text, ok := pk.(*v2193.TextPacket); ok {
			if text.Message != "echo hello" {
				t.Fatalf("echo %q", text.Message)
			}
			break
		}
	}
	if err := l.Disconnect(srv, "bye"); err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := conn.ReadPacket(); err != nil {
			if !errors.Is(err, ourclient.DisconnectError("bye")) {
				t.Fatalf("read after the disconnect: %v", err)
			}
			break
		}
	}
}
