package netconn

import (
	"compress/flate"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dm-vev/adamant/net/bedrock"
	bedrocklogin "github.com/dm-vev/adamant/net/bedrock/login"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/dm-vev/adamant/net/bedrock/resource"
	"github.com/dm-vev/adamant/net/bedrock/text"
	"github.com/dm-vev/adamant/net/bedrock/wire"
	"github.com/dm-vev/adamant/net/nethernet"
	"github.com/dm-vev/adamant/net/turn"
)

const (
	// defaultPacketRate and defaultPacketBurst bound the packets a client
	// sends a second: the game sends PlayerAuthInput 20 times a second and
	// a few more around it, a flood is cut off.
	defaultPacketRate, defaultPacketBurst = 500, 2048
	// defaultByteRate and defaultByteBurst bound the bytes a client sends
	// once decompressed: a login carries a skin of up to a few MiB.
	defaultByteRate, defaultByteBurst = 2 << 20, 16 << 20
	// loginTimeout bounds the login of a client, resource packs included.
	loginTimeout = time.Minute
	// statusInterval paces the updates of the status clients look up.
	statusInterval = 4 * time.Second
	// packChunkSize is the size of the chunks of a resource pack sent.
	packChunkSize = 128 << 10
	// keysTimeout bounds the fetch of the keys of the authorization
	// service a login may make.
	keysTimeout = 10 * time.Second
)

// Config configures a Listener.
type Config struct {
	// Log receives what goes wrong with connections. Nil discards it.
	Log *slog.Logger
	// MaximumPlayers bounds the players connected. Zero means no bound.
	MaximumPlayers int
	// StatusProvider gives the status of the server clients look up.
	StatusProvider StatusProvider
	// AuthenticationDisabled lets clients join without Xbox Live.
	AuthenticationDisabled bool
	// ResourcePacks are offered to every client; TexturePacksRequired makes
	// clients take them.
	ResourcePacks        []*resource.Pack
	TexturePacksRequired bool
	// Validate checks the identity and the client data of a client that
	// logged in, before Allow: the rules of the server for names, skins and
	// the rest. Nil takes them as they are.
	Validate func(identity bedrocklogin.Identity, client bedrocklogin.ClientData) error
	// Allow decides whether a client that logged in may join, and the
	// message of a refusal.
	Allow func(addr net.Addr, identity bedrocklogin.Identity, client bedrocklogin.ClientData) (string, bool)
	// Batches configures the batches of every connection: Compression is
	// announced to clients, Threshold and Incompressible decide which
	// batches are compressed, the rates bound what a client sends. Zero
	// values mean flate at level 1, a threshold of 256 bytes, every batch
	// over it compressed, and 500 packets and 2 MiB a second with bursts of
	// 2048 packets and 16 MiB.
	Batches     bedrock.Config
	Compression bedrock.Compression
}

// Listener accepts the clients of a nethernet.Listener and logs them in.
// A client logs in on a goroutine of its own, which ends once Accept can
// return it.
type Listener struct {
	cfg     Config
	batches bedrock.Config
	nn      *nethernet.Listener
	// keys verify the tokens of Xbox Live logins; nil without Xbox Live.
	keys     *bedrocklogin.Keys
	incoming chan *Conn
	players  atomic.Int32
	done     chan struct{}
	once     sync.Once
	wg       sync.WaitGroup

	staticMu sync.Mutex
	statics  map[staticKey][]byte

	// key signs the salt of the handshake: the key of the server identity,
	// or one made for the listener.
	key *ecdsa.PrivateKey
}

// Static is a packet that is the same for every connection, such as the
// recipes or the creative inventory. Its batch is encoded and compressed
// once and kept, by the identity of the packet, as long as the listener
// lives: a Static must not change after it is written.
type Static interface {
	proto.Packet
	// StaticPacket marks the packet; it does nothing.
	StaticPacket()
}

// staticKey names the batch of a Static encoded with a shield ID, which
// the items in it depend on.
type staticKey struct {
	pk     Static
	shield int32
}

// staticCompression compresses the batches of Statics: compressed once,
// they take the best level.
var staticCompression = bedrock.Flate{Level: flate.BestCompression, FixedBelow: -1}

// static returns the batch of a Static, encoding it the first time.
func (l *Listener) static(pk Static, shield int32) ([]byte, error) {
	l.staticMu.Lock()
	defer l.staticMu.Unlock()
	key := staticKey{pk, shield}
	if msg, ok := l.statics[key]; ok {
		return msg, nil
	}
	w := wire.Writer{B: bedrock.AppendHeader(nil, bedrock.Header{ID: pk.ID()})}
	pk.Encode(&w)
	comp := l.cfg.Compression
	if _, ok := comp.(bedrock.Flate); ok {
		comp = staticCompression
	}
	msg, err := bedrock.EncodeBatch(comp, w.B)
	if err != nil {
		return nil, err
	}
	if l.statics == nil {
		l.statics = make(map[staticKey][]byte)
	}
	l.statics[key] = msg
	return msg, nil
}

// Listen listens for clients on address with the transport configured by
// nn.
func (cfg Config) Listen(nn nethernet.ListenConfig, address string) (*Listener, error) {
	if cfg.Log == nil {
		cfg.Log = slog.New(slog.DiscardHandler)
	}
	if cfg.StatusProvider == nil {
		cfg.StatusProvider = NewStatusProvider("Minecraft Server", "")
	}
	if cfg.Compression == nil {
		cfg.Compression = bedrock.Flate{Level: 1}
	}
	l := &Listener{cfg: cfg, batches: cfg.Batches, incoming: make(chan *Conn), done: make(chan struct{})}
	if l.batches.PacketRate == 0 {
		l.batches.PacketRate, l.batches.PacketBurst = defaultPacketRate, defaultPacketBurst
	}
	if l.batches.ByteRate == 0 {
		l.batches.ByteRate, l.batches.ByteBurst = defaultByteRate, defaultByteBurst
	}
	if l.key = nn.Key; l.key == nil {
		var err error
		if l.key, err = ecdsa.GenerateKey(elliptic.P384(), rand.Reader); err != nil {
			return nil, err
		}
	}
	if !cfg.AuthenticationDisabled {
		var err error
		if l.keys, err = xboxKeys(); err != nil {
			return nil, fmt.Errorf("netconn: keys of Xbox Live logins: %w", err)
		}
	}
	var err error
	if l.nn, err = nn.Listen(address); err != nil {
		return nil, err
	}
	l.updateStatus()
	l.wg.Add(2)
	go l.accept()
	go l.refreshStatus()
	return l, nil
}

// xboxKeys returns the keys of the authorization service of the game,
// which signs the tokens of Xbox Live logins.
func xboxKeys() (*bedrocklogin.Keys, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return bedrocklogin.Discover(ctx, nil, v2193.Version)
}

// Accept returns the next client that logged in.
func (l *Listener) Accept() (*Conn, error) {
	select {
	case c := <-l.incoming:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

// Disconnect disconnects a client with a message; an empty message hides
// the screen of the disconnection.
func (l *Listener) Disconnect(c *Conn, message string) error {
	_ = c.WritePacket(&v2193.DisconnectPacket{HideDisconnectionScreen: message == "", Message: message})
	_ = c.Flush()
	return c.Close()
}

// Addr returns the address of the transport.
func (l *Listener) Addr() net.Addr { return l.nn.Addr() }

// Close stops accepting clients and closes the transport.
func (l *Listener) Close() error {
	l.once.Do(func() {
		close(l.done)
		_ = l.nn.Close()
		l.wg.Wait()
	})
	return nil
}

func (l *Listener) accept() {
	defer l.wg.Done()
	for {
		nn, err := l.nn.AcceptConn()
		if err != nil {
			return
		}
		go l.login(nn)
	}
}

func (l *Listener) refreshStatus() {
	defer l.wg.Done()
	t := time.NewTicker(statusInterval)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			l.updateStatus()
		case <-l.done:
			return
		}
	}
}

// Serve answers the offers that arrive over a signaling service, such as
// that of Xbox Live, until it ends.
func (l *Listener) Serve(s nethernet.Signaling) error { return l.nn.Serve(s) }

// Relay allocates a relayed address on a TURN server that clients behind
// other networks join through, announced in the answers of Serve.
func (l *Listener) Relay(ctx context.Context, cfg turn.Config) error { return l.nn.Relay(ctx, cfg) }

// ServerStatus returns the status the server shows clients.
func (l *Listener) ServerStatus() ServerStatus { return l.status() }

// Closed is closed once the listener closed.
func (l *Listener) Closed() <-chan struct{} { return l.done }

func (l *Listener) status() ServerStatus {
	s := l.cfg.StatusProvider.ServerStatus(int(l.players.Load()), l.cfg.MaximumPlayers)
	if s.MaxPlayers == 0 {
		s.MaxPlayers = s.PlayerCount + 1
	}
	return s
}

func (l *Listener) updateStatus() {
	s := l.status()
	l.nn.SetStatus(nethernet.Status{
		ServerName:     s.ServerName,
		Protocol:       v2193.Protocol,
		Version:        v2193.Version,
		LevelName:      s.ServerSubName,
		PlayerCount:    s.PlayerCount,
		MaxPlayerCount: s.MaxPlayers,
		GameType:       1,
	})
}

// left counts a client out.
func (l *Listener) left() {
	l.players.Add(-1)
	l.updateStatus()
}

// login logs a client in and queues it for Accept.
func (l *Listener) login(nn *nethernet.Conn) {
	c := newConn(l, nn)
	loginsStarted.Inc()
	if max := l.cfg.MaximumPlayers; max != 0 && int(l.players.Load()) >= max {
		loginsFull.Inc()
		_ = c.WritePacket(&v2193.PlayStatusPacket{Status: v2193.PlayStatusLoginFailedServerFull})
		_ = c.Flush()
		_ = nn.Close()
		return
	}
	l.players.Add(1)
	defer func() {
		// A panic is a bug of the login, which no client may use to take
		// the server down: the client is dropped.
		if v := recover(); v != nil {
			l.cfg.Log.Error("login panicked", "raddr", nn.RemoteAddr(), "panic", v)
			_ = c.Close()
		}
	}()
	l.updateStatus()
	_ = nn.SetReadDeadline(time.Now().Add(loginTimeout))
	if err := c.login(); err != nil {
		var closed DisconnectError
		if !errors.As(err, &closed) && !errors.Is(err, net.ErrClosed) {
			l.cfg.Log.Debug("login failed", "raddr", nn.RemoteAddr(), "err", err)
		}
		loginsFailed.Inc()
		_ = c.Close()
		return
	}
	loginsDone.Inc()
	loginSeconds.Observe(time.Since(c.connected).Seconds())
	_ = nn.SetReadDeadline(time.Time{})
	select {
	case l.incoming <- c:
	case <-l.done:
		_ = c.Close()
	}
}

// login runs the login of a client: network settings, the login request,
// the resource packs.
func (c *Conn) login() error {
	pk, err := c.read(v2193.IDRequestNetworkSettings)
	if err != nil {
		return err
	}
	if proto := pk.(*v2193.RequestNetworkSettingsPacket).ClientProtocol; proto != v2193.Protocol {
		status := v2193.PlayStatusLoginFailedClient
		if proto > v2193.Protocol {
			status = v2193.PlayStatusLoginFailedServer
		}
		_ = c.WritePacket(&v2193.PlayStatusPacket{Status: status})
		_ = c.Flush()
		return fmt.Errorf("netconn: client speaks protocol %d, not %d", proto, v2193.Protocol)
	}
	threshold := c.l.batches.Threshold
	if threshold == 0 {
		threshold = 256
	}
	_ = c.WritePacket(&v2193.NetworkSettingsPacket{
		CompressionThreshold: uint16(threshold),
		CompressionAlgorithm: c.l.cfg.Compression.Algorithm(),
	})
	if err := c.Flush(); err != nil {
		return err
	}
	c.b.EnableCompression(c.l.cfg.Compression)

	if pk, err = c.read(v2193.IDLogin); err != nil {
		return err
	}
	if err := c.authenticate(pk.(*v2193.LoginPacket)); err != nil {
		_ = c.Flush()
		return err
	}
	if err := c.handshake(); err != nil {
		return err
	}
	_ = c.WritePacket(&v2193.PlayStatusPacket{Status: v2193.PlayStatusLoginSuccess})
	_ = c.WritePacket(c.l.packsInfo())
	if err := c.Flush(); err != nil {
		return err
	}
	return c.sendPacks()
}

// handshake asks for the handshake that turns encryption on over RakNet,
// as BDS does over NetherNet too, where the connection is encrypted
// already: neither end turns anything on.
func (c *Conn) handshake() error {
	salt := make([]byte, 16)
	_, _ = rand.Read(salt)
	token, err := bedrocklogin.HandshakeToken(c.l.key, salt)
	if err != nil {
		return err
	}
	_ = c.WritePacket(&v2193.ServerToClientHandshakePacket{JWT: []byte(token)})
	if err := c.Flush(); err != nil {
		return err
	}
	_, err = c.read(v2193.IDClientToServerHandshake)
	return err
}

// authenticate checks the login request: the Xbox Live token if the server
// asks for it, the key the transport proved, and the server's own rules.
// The request is read by package login of the net module, which trusts
// none of it; the identity and the client data then pass the rules of the
// server, Validate.
func (c *Conn) authenticate(pk *v2193.LoginPacket) error {
	ctx, cancel := context.WithTimeout(context.Background(), keysTimeout)
	defer cancel()
	l, err := bedrocklogin.Parse(ctx, pk.ConnectionRequest, c.l.keys, time.Now())
	if err != nil {
		return fmt.Errorf("netconn: parse login request: %w", err)
	}
	c.identity = bedrocklogin.Identity{
		XUID:           l.Identity.XUID,
		Identity:       l.Identity.Identity,
		DisplayName:    l.Identity.DisplayName,
		TitleID:        l.Identity.TitleID,
		PlayFabID:      l.Identity.PlayFabID,
		PlayFabTitleID: l.Identity.PlayFabTitleID,
	}
	if err := json.Unmarshal(l.ClientDataJSON, &c.client); err != nil {
		return fmt.Errorf("netconn: client data: %w", err)
	}
	c.client.ServerAddress = l.ClientData.ServerAddress
	if validate := c.l.cfg.Validate; validate != nil {
		if err := validate(c.identity, c.client); err != nil {
			return fmt.Errorf("netconn: validate login: %w", err)
		}
	}
	if !l.XboxLive && !c.l.cfg.AuthenticationDisabled {
		_ = c.WritePacket(&v2193.DisconnectPacket{Message: text.Colourf("<red>You must be logged in with XBOX Live to join.</red>")})
		return errors.New("netconn: client is not logged in with Xbox Live")
	}
	if pub := c.nn.PublicKey(); pub != nil && !l.Key.Equal(pub) {
		// The transport proved another key: a login replayed elsewhere.
		_ = c.WritePacket(&v2193.DisconnectPacket{Reason: v2193.DisconnectReasonNotAuthenticated})
		return errors.New("netconn: login key is not the key of the transport identity")
	}
	if allow := c.l.cfg.Allow; allow != nil {
		if reason, ok := allow(c.RemoteAddr(), c.identity, c.client); !ok {
			_ = c.WritePacket(&v2193.DisconnectPacket{Reason: v2193.DisconnectReasonKicked, Message: reason})
			return DisconnectError("refused: " + reason)
		}
	}
	return nil
}

// packsInfo returns the resource packs offered.
func (l *Listener) packsInfo() *v2193.ResourcePacksInfoPacket {
	pk := &v2193.ResourcePacksInfoPacket{TexturePackRequired: l.cfg.TexturePacksRequired}
	for _, pack := range l.cfg.ResourcePacks {
		info := v2193.TexturePackInfo{UUID: pack.UUID(), Version: pack.Version(), Size: uint64(pack.Len()), DownloadURL: pack.DownloadURL()}
		if pack.Encrypted() {
			info.ContentKey = pack.ContentKey()
			info.ContentIdentity = pack.Manifest().Header.UUID.String()
		}
		pk.TexturePacks = append(pk.TexturePacks, info)
	}
	return pk
}

// sendPacks hands the client the resource packs it asks for, then the
// stack of packs, until it completes the login.
func (c *Conn) sendPacks() error {
	var (
		queue   []*resource.Pack
		current *resource.Pack
		offset  uint64
	)
	next := func() error {
		if len(queue) == 0 {
			return errors.New("netconn: no resource pack left to send")
		}
		current, offset, queue = queue[0], 0, queue[1:]
		checksum := current.Checksum()
		_ = c.WritePacket(&v2193.ResourcePackDataInfoPacket{
			UUID:          current.UUID().String() + "_" + current.Version(),
			DataChunkSize: packChunkSize,
			ChunkCount:    uint32(current.DataChunkCount(packChunkSize)),
			Size:          uint64(current.Len()),
			Hash:          checksum[:],
			PackType:      packType(current),
		})
		return c.Flush()
	}
	for {
		pk, err := c.read(v2193.IDResourcePackClientResponse, v2193.IDClientCacheStatus, v2193.IDResourcePackChunkRequest)
		if err != nil {
			return err
		}
		switch pk := pk.(type) {
		case *v2193.ClientCacheStatusPacket:
			c.cacheEnabled.Store(pk.Enabled)
		case *v2193.ResourcePackClientResponsePacket:
			switch pk.Response {
			case v2193.PackResponseRefused:
				return DisconnectError("refused the resource packs")
			case v2193.PackResponseSendPacks:
				if queue, err = c.l.packs(pk.PacksToDownload); err != nil {
					return err
				}
				if err := next(); err != nil {
					return err
				}
			case v2193.PackResponseAllPacksDownloaded:
				_ = c.WritePacket(c.l.packStack())
				if err := c.Flush(); err != nil {
					return err
				}
			case v2193.PackResponseCompleted:
				return nil
			default:
				return fmt.Errorf("netconn: unknown resource pack response %d", pk.Response)
			}
		case *v2193.ResourcePackChunkRequestPacket:
			id, _, _ := strings.Cut(pk.UUID, "_")
			if pk.ChunkIndex < 0 || current == nil || current.UUID().String() != id || uint64(pk.ChunkIndex)*packChunkSize != offset {
				return fmt.Errorf("netconn: unexpected request for chunk %d of resource pack %s", pk.ChunkIndex, pk.UUID)
			}
			data := make([]byte, packChunkSize)
			n, err := current.ReadAt(data, int64(offset))
			if err != nil && n == 0 {
				return fmt.Errorf("netconn: read resource pack: %w", err)
			}
			_ = c.WritePacket(&v2193.ResourcePackChunkDataPacket{UUID: pk.UUID, ChunkIndex: uint32(pk.ChunkIndex), DataOffset: offset, Data: data[:n]})
			if err := c.Flush(); err != nil {
				return err
			}
			if offset += uint64(n); offset >= uint64(current.Len()) && len(queue) > 0 {
				if err := next(); err != nil {
					return err
				}
			}
		}
	}
}

// packs returns the resource packs a client asks for by UUID, with or
// without a version.
func (l *Listener) packs(ids []string) ([]*resource.Pack, error) {
	var out []*resource.Pack
	for _, id := range ids {
		uuid, version, _ := strings.Cut(id, "_")
		found := false
		for _, pack := range l.cfg.ResourcePacks {
			if pack.UUID().String() == uuid && (version == "" || version == pack.Version()) {
				out, found = append(out, pack), true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("netconn: client asks for unknown resource pack %s", id)
		}
	}
	return out, nil
}

// packStack returns the stack of resource packs: the server's, then the
// ones every client has, as gophertunnel lists them.
func (l *Listener) packStack() *v2193.ResourcePackStackPacket {
	pk := &v2193.ResourcePackStackPacket{BaseGameVersion: v2193.Version, Experiments: []v2193.ExperimentData{{Name: "cameras", Enabled: true}}}
	for _, pack := range l.cfg.ResourcePacks {
		pk.TexturePacks = append(pk.TexturePacks, v2193.StackResourcePack{UUID: pack.UUID().String(), Version: pack.Version()})
	}
	for _, p := range builtinPacks {
		pk.TexturePacks = append(pk.TexturePacks, v2193.StackResourcePack{UUID: p[0], Version: p[1]})
	}
	return pk
}

// builtinPacks are the resource packs every client has, by UUID and
// version.
var builtinPacks = [][2]string{
	{"d34cfa4b-2ad1-453d-a0db-668b429a3ea0", "1.26.40"},
	{"b41c2785-c512-4a49-af56-3a87afd47c57", "1.21.30"},
	{"a4df0cb3-17be-4163-88d7-fcf7002b935d", "1.21.20"},
	{"d19adffe-a2e1-4b02-8436-ca4583368c89", "1.21.10"},
	{"85d5603d-2824-4b21-8044-34f441f4fce1", "1.21.0"},
	{"e977cd13-0a11-4618-96fb-03dfe9c43608", "1.20.60"},
	{"0674721c-a0aa-41a1-9ba8-1ed33ea3e7ed", "1.20.50"},
	{"0fba4063-dba1-4281-9b89-ff9390653530", "1.0.0"},
}

// packType returns the type of a resource pack, as ResourcePackDataInfo
// names it.
func packType(pack *resource.Pack) byte {
	switch {
	case pack.HasWorldTemplate():
		return v2193.ResourcePackTypeWorldTemplate
	case pack.HasTextures() && (pack.HasBehaviours() || pack.HasScripts()):
		return v2193.ResourcePackTypeAddon
	case !pack.HasTextures() && (pack.HasBehaviours() || pack.HasScripts()):
		return v2193.ResourcePackTypeBehaviour
	case pack.HasTextures():
		return v2193.ResourcePackTypeResources
	}
	return v2193.ResourcePackTypeSkins
}
