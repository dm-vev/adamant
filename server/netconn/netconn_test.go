package netconn

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha512"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dm-vev/adamant/net/bedrock"
	ourclient "github.com/dm-vev/adamant/net/bedrock/client"
	ourlogin "github.com/dm-vev/adamant/net/bedrock/login"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/dm-vev/adamant/net/bedrock/wire"
	"github.com/dm-vev/adamant/net/nethernet"
	"github.com/google/uuid"
)

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

func gameData() GameData {
	return GameData{
		WorldName:       "netconn",
		EntityUniqueID:  1,
		EntityRuntimeID: 1,
		PlayerPosition:  [3]float32{0, 64, 0},
		BaseGameVersion: v2193.Version,
		Items:           []v2193.ItemEntry{{Name: "minecraft:shield", RuntimeID: 355}},
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
			if text, ok := pk.(*v2193.TextPacket); ok {
				_ = c.WritePacket(&v2193.TextPacket{TextType: v2193.TextTypeRaw, Message: "echo " + text.Message})
				_ = c.Flush()
			}
		}
	}()
	return spawned
}

// TestLoginKeyMustMatchTransport refuses a Login whose key is not the one
// the transport identity proved: a login replayed on another connection.
func TestLoginKeyMustMatchTransport(t *testing.T) {
	l := listen(t, Config{})
	serve(t, l)
	b := rawClient(t, l)
	writeRaw(t, b, &v2193.RequestNetworkSettingsPacket{ClientProtocol: v2193.Protocol})
	if raw, err := b.ReadPacket(); err != nil {
		t.Fatal(err)
	} else if h, _, _ := bedrock.ReadHeader(raw); h.ID != v2193.IDNetworkSettings {
		t.Fatalf("packet %d instead of NetworkSettings", h.ID)
	}
	b.EnableCompression(bedrock.Flate{})
	other, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	request, err := ourlogin.EncodeOffline(ourlogin.Identity{DisplayName: "Replay", Identity: uuid.NewString()},
		ourlogin.DefaultClientData("127.0.0.1:19132", "Replay", v2193.Version), other, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	writeRaw(t, b, &v2193.LoginPacket{ClientProtocol: v2193.Protocol, ConnectionRequest: request})
	for {
		raw, err := b.ReadPacket()
		if err != nil {
			t.Fatalf("connection ended without a disconnect: %v", err)
		}
		h, _, _ := bedrock.ReadHeader(raw)
		if h.ID == v2193.IDDisconnect {
			break
		}
		if h.ID == v2193.IDPlayStatus || h.ID == v2193.IDStartGame {
			t.Fatalf("packet %d to a login the transport did not prove", h.ID)
		}
	}
}

// rawClient opens a NetherNet connection that speaks batches directly.
func rawClient(t *testing.T, l *Listener) *bedrock.Conn {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	nn, err := nethernet.Dialer{Key: key}.DialContext(ctx, l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = nn.Close() })
	_ = nn.SetReadDeadline(time.Now().Add(10 * time.Second))
	return bedrock.NewConn(nn, bedrock.Config{})
}

func writeRaw(t *testing.T, b *bedrock.Conn, pk proto.Packet) {
	t.Helper()
	w := wire.Writer{B: bedrock.AppendHeader(nil, bedrock.Header{ID: pk.ID()})}
	pk.Encode(&w)
	if err := b.WritePacket(w.B); err != nil {
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
	if err != nil || h.ID != v2193.IDPlayStatus {
		t.Fatalf("packet %d, %v", h.ID, err)
	}
	var pk v2193.PlayStatusPacket
	r := wire.NewReader(fields)
	pk.Decode(r)
	if r.Err() != nil {
		t.Fatal(r.Err())
	}
	return pk.Status
}

func TestProtocolMismatch(t *testing.T) {
	l := listen(t, Config{})
	for proto, want := range map[int32]int32{1: v2193.PlayStatusLoginFailedClient, v2193.Protocol + 1: v2193.PlayStatusLoginFailedServer} {
		b := rawClient(t, l)
		writeRaw(t, b, &v2193.RequestNetworkSettingsPacket{ClientProtocol: proto})
		if got := readStatus(t, b); got != want {
			t.Fatalf("protocol %d: status %d, want %d", proto, got, want)
		}
	}
}

func TestServerFull(t *testing.T) {
	l := listen(t, Config{MaximumPlayers: 1})
	serve(t, l)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	conn, err := ourclient.Dial(ctx, l.Addr().String(), ourclient.Config{Identity: ourlogin.Identity{DisplayName: "First"}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	b := rawClient(t, l)
	if got := readStatus(t, b); got != v2193.PlayStatusLoginFailedServerFull {
		t.Fatalf("status %d for the second client", got)
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
	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()
	for _, msg := range []string{"hello", strings.Repeat("long ", 300)} {
		if err := conn.WritePacket(&v2193.TextPacket{TextType: 1, SourceName: "Bot", Message: msg}); err != nil {
			t.Fatal(err)
		}
		if err := conn.Flush(); err != nil {
			t.Fatal(err)
		}
		for {
			pk, err := conn.ReadPacket()
			if err != nil {
				t.Fatal(err)
			}
			if text, ok := pk.(*v2193.TextPacket); ok {
				if text.Message != "echo "+msg {
					t.Fatalf("echo %q", text.Message)
				}
				break
			}
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

// staticText is a Text the same for every connection.
type staticText struct{ proto.Packet }

func (*staticText) StaticPacket() {}

// TestStaticPacketsEncodeOnce sends one Static to two clients: both read
// it, from a batch encoded once.
func TestStaticPacketsEncodeOnce(t *testing.T) {
	l := listen(t, Config{})
	st := &staticText{&v2193.TextPacket{TextType: v2193.TextTypeRaw, Message: strings.Repeat("static ", 100)}}
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
				_ = c.WritePacket(&v2193.TextPacket{TextType: v2193.TextTypeRaw, Message: "first"})
				_ = c.WritePacket(st)
				_ = c.Flush()
			}()
		}
	}()
	for range 2 {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		conn, err := ourclient.Dial(ctx, l.Addr().String(), ourclient.Config{})
		if err != nil {
			t.Fatal(err)
		}
		stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
		var got []string
		for len(got) < 2 {
			pk, err := conn.ReadPacket()
			if err != nil {
				t.Fatal(err)
			}
			if text, ok := pk.(*v2193.TextPacket); ok {
				got = append(got, text.Message)
			}
		}
		stop()
		cancel()
		_ = conn.Close()
		if got[0] != "first" || got[1] != st.Packet.(*v2193.TextPacket).Message {
			t.Fatalf("read %q", got)
		}
	}
	l.staticMu.Lock()
	defer l.staticMu.Unlock()
	if len(l.statics) != 1 {
		t.Fatalf("%d static batches, want 1", len(l.statics))
	}
}

// TestFloodingClientIsCut reads a client that sends far more packets than
// a game does until the read fails.
func TestFloodingClientIsCut(t *testing.T) {
	l := listen(t, Config{})
	result := make(chan error, 1)
	go func() {
		c, err := l.Accept()
		if err != nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := c.StartGameContext(ctx, gameData()); err != nil {
			result <- err
			return
		}
		for {
			if _, err := c.ReadPacket(); err != nil {
				result <- err
				return
			}
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	conn, err := ourclient.Dial(ctx, l.Addr().String(), ourclient.Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	for range 100 {
		for range 100 {
			_ = conn.WritePacket(&v2193.TextPacket{TextType: v2193.TextTypeChat, SourceName: "Player", Message: "spam"})
		}
		if err := conn.Flush(); err != nil {
			break
		}
	}
	select {
	case err := <-result:
		if !errors.Is(err, bedrock.ErrRateLimited) {
			t.Fatalf("read ended with %v, want %v", err, bedrock.ErrRateLimited)
		}
	case <-ctx.Done():
		t.Fatal("the flood was not cut off")
	}
}

// TestChatOverItsBucketIsDropped reads 100 Text packets sent at once: as
// BDS does, the server takes the 50 their bucket holds and drops the rest,
// and reads the packets of other kinds after them.
func TestChatOverItsBucketIsDropped(t *testing.T) {
	l := listen(t, Config{})
	texts := make(chan int, 1)
	go func() {
		c, err := l.Accept()
		if err != nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := c.StartGameContext(ctx, gameData()); err != nil {
			_ = c.Close()
			return
		}
		n := 0
		for {
			pk, err := c.ReadPacket()
			if err != nil {
				return
			}
			switch pk.(type) {
			case *v2193.TextPacket:
				n++
			case *v2193.EmotePacket:
				texts <- n
				return
			}
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	conn, err := ourclient.Dial(ctx, l.Addr().String(), ourclient.Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	for range 100 {
		_ = conn.WritePacket(&v2193.TextPacket{TextType: v2193.TextTypeChat, SourceName: "Player", Message: "spam"})
	}
	_ = conn.WritePacket(&v2193.EmotePacket{EntityRuntimeID: 1, EmoteID: "wave"})
	if err := conn.Flush(); err != nil {
		t.Fatal(err)
	}
	select {
	case n := <-texts:
		// The bucket drains 10 a second while the packets go.
		if n < 50 || n > 55 {
			t.Fatalf("read %d of 100 Text packets, want 50", n)
		}
	case <-ctx.Done():
		t.Fatal("the server did not read past the chat")
	}
}

// TestHostileLoginRequests sends connection requests a client may forge:
// each fails the login of that client alone, and the server logs others
// in after them.
func TestHostileLoginRequests(t *testing.T) {
	l := listen(t, Config{})
	spawned := serve(t, l)
	head := []byte(`{"Certificate":"{\"chain\":[\"\"]}","AuthenticationType":2,"Token":"x"}`)
	part := func(b []byte, n uint32, data []byte) []byte {
		return append(binary.LittleEndian.AppendUint32(b, n), data...)
	}
	for name, req := range map[string][]byte{
		// gophertunnel's login.Parse panics on a negative length.
		"negative client data length": part(part(nil, uint32(len(head)), head), 0xffffffff, nil),
		"client data past the end":    part(part(nil, uint32(len(head)), head), 1<<20, []byte("x")),
		"negative certificate length": part(nil, 0x80000000, head),
		"empty":                       nil,
		"garbage":                     []byte("\x05\x00\x00\x00{}{}{"),
	} {
		if err := loginWith(t, l, req); err == nil {
			t.Fatalf("%s: logged in", name)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	conn, err := ourclient.Dial(ctx, l.Addr().String(), ourclient.Config{})
	if err != nil {
		t.Fatalf("dial after the hostile logins: %v", err)
	}
	defer conn.Close()
	select {
	case <-spawned:
	case <-ctx.Done():
		t.Fatal("server did not spawn the client after the hostile logins")
	}
}

// TestLargeLoginDecodes decodes a Login of a connection request past the
// bound strings have by default: a skin the game makes can take that much.
func TestLargeLoginDecodes(t *testing.T) {
	req := make([]byte, 3<<20)
	w := wire.Writer{B: bedrock.AppendHeader(nil, bedrock.Header{ID: v2193.IDLogin})}
	(&v2193.LoginPacket{ClientProtocol: v2193.Protocol, ConnectionRequest: req}).Encode(&w)
	pk, err := DecodePacket(w.B)
	if err != nil {
		t.Fatal(err)
	}
	if l := pk.(*v2193.LoginPacket); len(l.ConnectionRequest) != len(req) {
		t.Fatalf("connection request of %d bytes", len(l.ConnectionRequest))
	}
	// A length past the packet is still refused.
	if _, err := DecodePacket(w.B[:len(w.B)-1]); err == nil {
		t.Fatal("decoded a connection request past the end of its packet")
	}
}

// loginWith sends a Login of a connection request and returns the error
// that ends the connection, nil if the server went on with the login.
func loginWith(t *testing.T, l *Listener, req []byte) error {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	nn, err := nethernet.Dialer{Key: key}.DialContext(ctx, l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer nn.Close()
	_ = nn.SetReadDeadline(time.Now().Add(10 * time.Second))
	b := bedrock.NewConn(nn, bedrock.Config{})
	send := func(pk proto.Packet) {
		w := wire.Writer{B: bedrock.AppendHeader(nil, bedrock.Header{ID: pk.ID()})}
		pk.Encode(&w)
		_ = b.WritePacket(w.B)
		_ = b.Flush()
	}
	send(&v2193.RequestNetworkSettingsPacket{ClientProtocol: v2193.Protocol})
	if _, err := b.ReadPacket(); err != nil {
		t.Fatal(err)
	}
	b.EnableCompression(bedrock.Flate{})
	send(&v2193.LoginPacket{ClientProtocol: v2193.Protocol, ConnectionRequest: req})
	for {
		raw, err := b.ReadPacket()
		if err != nil {
			return err
		}
		if h, _, _ := bedrock.ReadHeader(raw); h.ID == v2193.IDServerToClientHandshake {
			return nil
		}
	}
}

// TestHandshakeAsBDS logs in packet by packet: as BDS does, the server
// asks for the handshake after the login, with a salt signed by the key it
// names, and goes on once the client answers.
func TestHandshakeAsBDS(t *testing.T) {
	l := listen(t, Config{})
	serve(t, l)
	key, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	nn, err := nethernet.Dialer{Key: key}.DialContext(ctx, l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer nn.Close()
	_ = nn.SetReadDeadline(time.Now().Add(10 * time.Second))
	b := bedrock.NewConn(nn, bedrock.Config{})
	send := func(pk proto.Packet) {
		w := wire.Writer{B: bedrock.AppendHeader(nil, bedrock.Header{ID: pk.ID()})}
		pk.Encode(&w)
		if err := b.WritePacket(w.B); err != nil {
			t.Fatal(err)
		}
		if err := b.Flush(); err != nil {
			t.Fatal(err)
		}
	}
	next := func() proto.Packet {
		raw, err := b.ReadPacket()
		if err != nil {
			t.Fatal(err)
		}
		h, body, _ := bedrock.ReadHeader(raw)
		pk := v2193.New(h.ID)
		r := wire.NewReader(body)
		pk.Decode(r)
		if r.Err() != nil {
			t.Fatalf("decode %T: %v", pk, r.Err())
		}
		return pk
	}
	send(&v2193.RequestNetworkSettingsPacket{ClientProtocol: v2193.Protocol})
	if _, ok := next().(*v2193.NetworkSettingsPacket); !ok {
		t.Fatal("no NetworkSettings")
	}
	b.EnableCompression(bedrock.Flate{})
	req, err := ourlogin.EncodeOffline(ourlogin.Identity{DisplayName: "Bot", Identity: ourlogin.NewUUID()},
		ourlogin.DefaultClientData(l.Addr().String(), "Bot", v2193.Version), key, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	send(&v2193.LoginPacket{ClientProtocol: v2193.Protocol, ConnectionRequest: req})
	pk := next()
	hs, ok := pk.(*v2193.ServerToClientHandshakePacket)
	if !ok {
		t.Fatalf("%T after the login, want ServerToClientHandshake", pk)
	}
	parts := strings.Split(string(hs.JWT), ".")
	if len(parts) != 3 {
		t.Fatalf("token %q", hs.JWT)
	}
	header, _ := base64.RawURLEncoding.DecodeString(parts[0])
	var h struct{ Alg, X5U string }
	_ = json.Unmarshal(header, &h)
	der, _ := base64.StdEncoding.DecodeString(h.X5U)
	pub, err := x509.ParsePKIXPublicKey(der)
	server, isKey := pub.(*ecdsa.PublicKey)
	sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
	sum := sha512.Sum384([]byte(parts[0] + "." + parts[1]))
	if h.Alg != "ES384" || err != nil || !isKey || len(sig) != 96 ||
		!ecdsa.Verify(server, sum[:], new(big.Int).SetBytes(sig[:48]), new(big.Int).SetBytes(sig[48:])) {
		t.Fatalf("token not signed by the key it names: header %s", header)
	}
	payload, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var claims struct{ Salt string }
	_ = json.Unmarshal(payload, &claims)
	if salt, err := base64.RawStdEncoding.DecodeString(claims.Salt); err != nil || len(salt) != 16 {
		t.Fatalf("salt %q", claims.Salt)
	}
	send(&v2193.ClientToServerHandshakePacket{})
	if ps, ok := next().(*v2193.PlayStatusPacket); !ok || ps.Status != 0 {
		t.Fatalf("no login success after the handshake")
	}
}

// TestReceive hands the packets of a spawned client to a function on the
// goroutine of the transport, in order and the ones read during the spawn
// first, and tells of the close once the client left.
func TestReceive(t *testing.T) {
	l := listen(t, Config{})
	got := make(chan string, 16)
	closed := make(chan error, 1)
	go func() {
		c, err := l.Accept()
		if err != nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := c.StartGameContext(ctx, gameData()); err != nil {
			_ = c.Close()
			return
		}
		c.Receive(func(pk proto.Packet) {
			if text, ok := pk.(*v2193.TextPacket); ok {
				got <- text.Message
			}
		}, func(err error) { closed <- err })
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	conn, err := ourclient.Dial(ctx, l.Addr().String(), ourclient.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for i := range 5 {
		_ = conn.WritePacket(&v2193.TextPacket{TextType: v2193.TextTypeChat, Message: fmt.Sprint(i)})
		if i%2 == 0 {
			_ = conn.Flush()
		}
	}
	_ = conn.Flush()
	for i := range 5 {
		select {
		case msg := <-got:
			if msg != fmt.Sprint(i) {
				t.Fatalf("message %d is %q", i, msg)
			}
		case <-ctx.Done():
			t.Fatal("packets did not arrive")
		}
	}
	_ = conn.Close()
	select {
	case err := <-closed:
		if err == nil {
			t.Fatal("closed without a reason")
		}
	case <-ctx.Done():
		t.Fatal("the close was not told")
	}
}

// held returns what decoding raw returns and the bytes of memory it holds.
func held(raw []byte) (proto.Packet, uint64, error) {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	pk, err := DecodePacket(raw)
	runtime.GC()
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(pk)
	if after.HeapAlloc < before.HeapAlloc {
		return pk, 0, err
	}
	return pk, after.HeapAlloc - before.HeapAlloc, err
}

// blockActorData returns a BlockActorData of NBT of n lists of 1024 empty
// compounds, each a byte to send and 64 bytes to hold.
func blockActorData(n int) []byte {
	w := wire.Writer{B: bedrock.AppendHeader(nil, bedrock.Header{ID: v2193.IDBlockActorData})}
	w.Bytes([]byte{0, 0, 0})      // position
	w.Bytes([]byte{10, 0})        // root compound
	w.Bytes([]byte{9, 1, 'a', 9}) // "a", a list of lists
	w.Varint32(int32(n))
	for range n {
		w.Bytes([]byte{10})
		w.Varint32(1024)
		w.Bytes(make([]byte, 1024))
	}
	w.Uint8(0)
	return w.B
}

// TestNBTMemoryBound decodes BlockActorData of NBT that takes 64 times its
// bytes to hold, as 8 MiB of empty compounds held 512 MiB: past the memory
// a packet may take it is refused, within it it decodes.
func TestNBTMemoryBound(t *testing.T) {
	raw := blockActorData(128)
	_, n, err := held(raw)
	if !errors.Is(err, wire.ErrAlloc) {
		t.Errorf("decoded %d bytes into %d held: %v", len(raw), n, err)
	}
	if n > 1<<20 {
		t.Errorf("a refused packet of %d bytes holds %d", len(raw), n)
	}
	pk, err := DecodePacket(blockActorData(16))
	if err != nil {
		t.Fatal(err)
	}
	if l := pk.(*v2193.BlockActorDataPacket).NBTData["a"].([]any); len(l) != 16 {
		t.Fatalf("decoded %d lists", len(l))
	}
	// One list of more compounds than a list may hold.
	w := wire.Writer{B: bedrock.AppendHeader(nil, bedrock.Header{ID: v2193.IDBlockActorData})}
	w.Bytes([]byte{0, 0, 0, 10, 0, 9, 1, 'a', 10})
	w.Varint32(maxList + 1)
	w.Bytes(make([]byte, maxList+2))
	if _, err := DecodePacket(w.B); !errors.Is(err, wire.ErrTooLarge) {
		t.Fatalf("decoded a list of %d compounds: %v", maxList+1, err)
	}
}

// TestServerPacketsRefused decodes packets only a server sends, crafted to
// hold far more than their bytes: they are refused for their ID, as a
// ClientBoundDataStore of 1 KiB held 248 KiB and a SubChunk 57 times its
// bytes.
func TestServerPacketsRefused(t *testing.T) {
	store := wire.Writer{B: bedrock.AppendHeader(nil, bedrock.Header{ID: v2193.IDClientBoundDataStore})}
	store.Varuint32(1024)
	store.Bytes(bytes.Repeat([]byte{3}, 1024))
	sub := wire.Writer{B: bedrock.AppendHeader(nil, bedrock.Header{ID: v2193.IDSubChunk})}
	sub.Bytes(make([]byte, 14))
	sub.Varuint32(1024)
	sub.Bytes(make([]byte, 10*1024))
	for _, raw := range [][]byte{store.B, sub.B} {
		pk, n, err := held(raw)
		if !errors.As(err, new(errUnknownPacket)) {
			t.Errorf("decoded %T of %d bytes into %d held: %v", pk, len(raw), n, err)
		}
	}
}

// TestLoginPacketsDecode decodes each packet the connection reads of a
// client itself, from the login to the spawn: none is refused for its ID.
func TestLoginPacketsDecode(t *testing.T) {
	for _, pk := range []proto.Packet{
		&v2193.RequestNetworkSettingsPacket{ClientProtocol: v2193.Protocol},
		&v2193.LoginPacket{ClientProtocol: v2193.Protocol},
		&v2193.ClientToServerHandshakePacket{},
		&v2193.ClientCacheStatusPacket{},
		&v2193.ResourcePackClientResponsePacket{Response: v2193.PackResponseCompleted},
		&v2193.ResourcePackChunkRequestPacket{},
		&v2193.RequestChunkRadiusPacket{ChunkRadius: 8},
		&v2193.SetLocalPlayerAsInitialisedPacket{},
		&v2193.DisconnectPacket{Message: "bye"},
	} {
		w := wire.Writer{B: bedrock.AppendHeader(nil, bedrock.Header{ID: pk.ID()})}
		pk.Encode(&w)
		got, err := DecodePacket(w.B)
		if err != nil || got.ID() != pk.ID() {
			t.Errorf("%T: %v", pk, err)
		}
	}
}

// itemStackRequest returns an ItemStackRequest of n requests, each of an
// action of 1024 empty craft results and of 1024 empty filter strings:
// 7 bytes to send an item and a string, 112 to hold them.
func itemStackRequest(n int) []byte {
	w := wire.Writer{B: bedrock.AppendHeader(nil, bedrock.Header{ID: v2193.IDItemStackRequest})}
	w.Varuint32(uint32(n))
	for range n {
		w.Varint32(0)                 // request ID
		w.Varuint32(1)                // one action
		w.Bytes([]byte{17, 19})       // of craft results, deprecated
		w.Varuint32(1024)             // results
		w.Bytes(make([]byte, 6*1024)) // each empty
		w.Uint8(0)                    // times crafted
		w.Varuint32(1024)             // filter strings
		w.Bytes(make([]byte, 1024))   // each empty
		w.Int32(0)                    // filter cause
	}
	return w.B
}

// craftActions returns an ItemStackRequest of n requests of 1024 actions of
// no craft results: 4 bytes to send an action, a slot of 16 bytes and a
// struct of 32 to hold it.
func craftActions(n int) []byte {
	w := wire.Writer{B: bedrock.AppendHeader(nil, bedrock.Header{ID: v2193.IDItemStackRequest})}
	w.Varuint32(uint32(n))
	for range n {
		w.Varint32(0)
		w.Varuint32(1024)
		for range 1024 {
			w.Bytes([]byte{17, 19, 0, 0})
		}
		w.Varuint32(0)
		w.Int32(0)
	}
	return w.B
}

// TestListMemoryBound decodes ItemStackRequests of lists in lists, each
// within maxList, that hold 12 to 16 times their bytes, as 7 MiB of them
// held 114 MiB: past the memory a packet may take they are refused,
// within it they decode.
func TestListMemoryBound(t *testing.T) {
	for _, raw := range [][]byte{itemStackRequest(80), craftActions(128)} {
		_, n, err := held(raw)
		if !errors.Is(err, wire.ErrAlloc) {
			t.Errorf("decoded %d bytes into %d held: %v", len(raw), n, err)
		}
		if n > 1<<20 {
			t.Errorf("a refused packet of %d bytes holds %d", len(raw), n)
		}
	}
	if _, err := DecodePacket(craftActions(16)); err != nil {
		t.Fatal(err)
	}
	pk, err := DecodePacket(itemStackRequest(8))
	if err != nil {
		t.Fatal(err)
	}
	if req := pk.(*v2193.ItemStackRequestPacket).Requests; len(req) != 8 || len(req[7].FilterStrings) != 1024 {
		t.Fatalf("decoded %d requests", len(req))
	}
}

// mobEquipment returns a MobEquipment of an item of the NBT and the blocks
// to be placed on passed, as a client sends it.
func mobEquipment(data map[string]any, placeOn []string) []byte {
	pk := &v2193.MobEquipmentPacket{NewItem: v2193.ItemInstance{Stack: v2193.ItemStack{
		NetworkID: 1, Count: 1, NBTData: data, CanBePlacedOn: placeOn,
	}}}
	w := wire.Writer{B: bedrock.AppendHeader(nil, bedrock.Header{ID: pk.ID()})}
	pk.Encode(&w)
	return w.B
}

// emptyCompounds returns n lists of 1024 empty compounds.
func emptyCompounds(n int) []any {
	l := make([]any, n)
	for i := range l {
		c := make([]any, 1024)
		for j := range c {
			c[j] = map[string]any{}
		}
		l[i] = c
	}
	return l
}

// TestItemDataBound decodes MobEquipments of items whose user data holds
// far more than its bytes, as 8 MiB of it held 512 MiB of empty compounds
// or 65 MiB of empty strings: the data is read with the limits and the
// memory budget of its packet.
func TestItemDataBound(t *testing.T) {
	for name, raw := range map[string][]byte{
		"compounds": mobEquipment(map[string]any{"a": emptyCompounds(128)}, nil),
		"strings":   mobEquipment(nil, make([]string, maxList+1)),
	} {
		_, n, err := held(raw)
		if err == nil {
			t.Errorf("%s: decoded %d bytes into %d held", name, len(raw), n)
		}
		if n > 1<<20 {
			t.Errorf("%s: a refused packet of %d bytes holds %d", name, len(raw), n)
		}
	}
	pk, err := DecodePacket(mobEquipment(map[string]any{"a": emptyCompounds(16)}, make([]string, maxList)))
	if err != nil {
		t.Fatal(err)
	}
	st := pk.(*v2193.MobEquipmentPacket).NewItem.Stack
	if len(st.NBTData["a"].([]any)) != 16 || len(st.CanBePlacedOn) != maxList {
		t.Fatalf("decoded %d lists, %d blocks", len(st.NBTData["a"].([]any)), len(st.CanBePlacedOn))
	}
}

// TestShortErrors decodes a CommandRequest of an origin of a name of 1 MiB
// that no origin has: the error names it short, as one that quoted it
// whole took 29 times its bytes to build, 231 MiB for 8 MiB.
func TestShortErrors(t *testing.T) {
	w := wire.Writer{B: bedrock.AppendHeader(nil, bedrock.Header{ID: v2193.IDCommandRequest})}
	w.String("/help")
	w.ByteSlice(make([]byte, 1<<20)) // the name of the origin
	w.Bytes(make([]byte, 16+1+8+1+1))
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	_, err := DecodePacket(w.B)
	runtime.ReadMemStats(&after)
	if err == nil || !strings.Contains(err.Error(), "commandOrigin") {
		t.Fatalf("decoded an unknown origin: %v", err)
	}
	if len(err.Error()) > 512 {
		t.Errorf("an error of %d bytes", len(err.Error()))
	}
	if n := after.TotalAlloc - before.TotalAlloc; n > 2*uint64(len(w.B)) {
		t.Errorf("decoding %d bytes allocated %d", len(w.B), n)
	}
}
