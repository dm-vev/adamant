// Package client joins Bedrock servers over NetherNet as the game does,
// with the codecs of protocol 2193: it logs in offline or with a token of
// an account, takes no resource packs, spawns, and then reads and writes
// packets.
package client

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/dm-vev/adamant/net/bedrock"
	"github.com/dm-vev/adamant/net/bedrock/login"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/dm-vev/adamant/net/bedrock/wire"
	"github.com/dm-vev/adamant/net/nethernet"
)

// Config configures how a client joins.
type Config struct {
	// Identity names the player; a DisplayName of "Player" and a random
	// UUID are used for those left empty.
	Identity login.Identity
	// ClientData describes the device and the skin. Nil means
	// login.DefaultClientData.
	ClientData *login.ClientData
	// Key signs the login and the identity of the transport, which the
	// server matches. A new one is made if nil.
	Key *ecdsa.PrivateKey
	// ChunkRadius is the radius of the chunks asked for; zero means 8.
	ChunkRadius int32
	// Dialer opens the transport. Its Key is set to Key.
	Dialer nethernet.Dialer
	// Batches configures the batches; compression follows the server.
	Batches bedrock.Config
	// Token, if set, returns the multiplayer token of an account issued
	// for the public key of Key, which the client then logs in with as a
	// player of Xbox Live instead of naming itself offline.
	Token func(ctx context.Context, key *ecdsa.PublicKey) (string, error)
}

// Conn is a client spawned in a server.
type Conn struct {
	nn   *nethernet.Conn
	b    *bedrock.Conn
	game v2193.StartGamePacket

	// deferred holds the packets read during the spawn, read first.
	deferred [][]byte
	// items is the item registry the server sent during the spawn.
	items []v2193.ItemEntry

	writeMu sync.Mutex
	w       wire.Writer
}

// Play statuses of PlayStatus.
const (
	loginSuccess = 0
	playerSpawn  = 3
)

// Responses of ResourcePackClientResponse.
const (
	allPacksDownloaded = 3
	packsCompleted     = 4
)

// DisconnectError is the message of a server that ended the connection.
type DisconnectError string

func (e DisconnectError) Error() string { return "client: disconnected: " + string(e) }

// Dial joins the server at address, a signaling URL or host:port, and
// returns once the player spawned.
func Dial(ctx context.Context, address string, cfg Config) (*Conn, error) {
	if cfg.Key == nil {
		var err error
		if cfg.Key, err = ecdsa.GenerateKey(elliptic.P384(), rand.Reader); err != nil {
			return nil, err
		}
	}
	if cfg.Identity.DisplayName == "" {
		cfg.Identity.DisplayName = "Player"
	}
	if cfg.Identity.Identity == "" {
		cfg.Identity.Identity = login.NewUUID()
	}
	data := login.DefaultClientData(serverAddress(address), cfg.Identity.DisplayName, v2193.Version)
	if cfg.ClientData != nil {
		data = *cfg.ClientData
	}
	if cfg.ChunkRadius == 0 {
		cfg.ChunkRadius = 8
	}
	var request []byte
	if cfg.Token != nil {
		token, err := cfg.Token(ctx, &cfg.Key.PublicKey)
		if err != nil {
			return nil, fmt.Errorf("client: multiplayer token: %w", err)
		}
		if request, err = login.EncodeToken(token, data, cfg.Key); err != nil {
			return nil, err
		}
		// The offer names the player with the same token.
		cfg.Dialer.Token = token
	} else {
		var err error
		if request, err = login.EncodeOffline(cfg.Identity, data, cfg.Key, time.Now()); err != nil {
			return nil, err
		}
	}
	cfg.Dialer.Key = cfg.Key
	nn, err := cfg.Dialer.DialContext(ctx, address)
	if err != nil {
		return nil, err
	}
	c := &Conn{nn: nn, b: bedrock.NewConn(nn, cfg.Batches)}
	// The context bounds the login: once done, reads fail.
	stop := context.AfterFunc(ctx, func() { _ = nn.SetReadDeadline(time.Now()) })
	err = c.join(cfg, request)
	if !stop() || err != nil {
		_ = nn.Close()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	return c, nil
}

// serverAddress returns the host and port of an address, as the game
// names the server it joins in its client data: a signaling URL would not
// pass for one.
func serverAddress(address string) string {
	if u, err := url.Parse(address); err == nil && u.Host != "" {
		return u.Host
	}
	return address
}

// join logs in and spawns.
func (c *Conn) join(cfg Config, request []byte) error {
	if err := c.send(&v2193.RequestNetworkSettingsPacket{ClientProtocol: v2193.Protocol}); err != nil {
		return err
	}
	pk, _, err := c.next()
	if err != nil {
		return err
	}
	settings, ok := pk.(*v2193.NetworkSettingsPacket)
	if !ok {
		return fmt.Errorf("client: %T instead of NetworkSettings", pk)
	}
	switch settings.CompressionAlgorithm {
	case bedrock.AlgorithmFlate:
		c.b.EnableCompression(bedrock.Flate{})
	case bedrock.AlgorithmNone:
		c.b.EnableCompression(bedrock.None{})
	default:
		return fmt.Errorf("client: compression %d is not supported", settings.CompressionAlgorithm)
	}
	if err := c.send(&v2193.LoginPacket{ClientProtocol: v2193.Protocol, ConnectionRequest: request}); err != nil {
		return err
	}
	started := false
	for {
		pk, raw, err := c.next()
		if err != nil {
			return err
		}
		switch pk := pk.(type) {
		case *v2193.PlayStatusPacket:
			switch pk.Status {
			case loginSuccess:
				continue
			case playerSpawn:
				if !started {
					return errors.New("client: spawned before StartGame")
				}
				return c.send(&v2193.SetLocalPlayerAsInitialisedPacket{EntityRuntimeID: c.game.EntityRuntimeID})
			default:
				return fmt.Errorf("client: login failed with status %d", pk.Status)
			}
		case *v2193.ServerToClientHandshakePacket:
			// BDS asks for the handshake that turns encryption on over RakNet
			// even over NetherNet, which encrypts the connection itself: the
			// answer turns nothing on.
			err = c.send(&v2193.ClientToServerHandshakePacket{})
		case *v2193.ResourcePacksInfoPacket:
			err = c.send(&v2193.ResourcePackClientResponsePacket{Response: allPacksDownloaded})
		case *v2193.ResourcePackStackPacket:
			err = c.send(&v2193.ResourcePackClientResponsePacket{Response: packsCompleted})
		case *v2193.ItemRegistryPacket:
			c.items = pk.Items
			// Shields carry one more field, so their ID must be known.
			for _, item := range pk.Items {
				if item.Name == "minecraft:shield" {
					v2193.SetShieldID(int32(item.RuntimeID))
				}
			}
			if started {
				c.deferred = append(c.deferred, raw)
			}
		case *v2193.StartGamePacket:
			c.game, started = *pk, true
			err = c.send(&v2193.RequestChunkRadiusPacket{ChunkRadius: cfg.ChunkRadius, MaxChunkRadius: uint8(min(cfg.ChunkRadius, 255))})
		default:
			if started {
				c.deferred = append(c.deferred, raw)
			}
		}
		if err != nil {
			return err
		}
	}
}

// next reads and decodes the next packet, failing on a Disconnect or on
// a packet that does not decode.
func (c *Conn) next() (proto.Packet, []byte, error) {
	raw, err := c.b.ReadPacket()
	if err != nil {
		return nil, nil, err
	}
	pk, err := decode(raw)
	if err != nil {
		return nil, nil, err
	}
	if d, ok := pk.(*v2193.DisconnectPacket); ok {
		return nil, nil, DisconnectError(d.Message)
	}
	return pk, raw, nil
}

// DecodeError is a packet that does not decode: the connection goes on.
type DecodeError struct {
	ID  uint32
	Err error
}

func (e *DecodeError) Error() string {
	return fmt.Sprintf("client: decode packet %d: %v", e.ID, e.Err)
}

func (e *DecodeError) Unwrap() error { return e.Err }

func decode(raw []byte) (proto.Packet, error) {
	h, body, err := bedrock.ReadHeader(raw)
	if err != nil {
		return nil, err
	}
	pk := v2193.New(h.ID)
	if pk == nil {
		return nil, &DecodeError{ID: h.ID, Err: errors.New("unknown packet")}
	}
	r := wire.NewReader(body)
	pk.Decode(r)
	if r.Err() == nil && r.Len() != 0 {
		r.Failf("%d bytes left", r.Len())
	}
	if r.Err() != nil {
		return nil, &DecodeError{ID: h.ID, Err: r.Err()}
	}
	return pk, nil
}

// StartGame returns the StartGame packet the server spawned the player
// with.
func (c *Conn) StartGame() *v2193.StartGamePacket { return &c.game }

// Items returns the item registry the server sent during the spawn.
func (c *Conn) Items() []v2193.ItemEntry { return c.items }

// ReadPacket returns the next packet of the server. A *DecodeError leaves
// the connection usable; a Disconnect ends it with a DisconnectError.
func (c *Conn) ReadPacket() (proto.Packet, error) {
	var raw []byte
	if len(c.deferred) > 0 {
		raw, c.deferred = c.deferred[0], c.deferred[1:]
	} else {
		var err error
		if raw, err = c.b.ReadPacket(); err != nil {
			return nil, err
		}
	}
	pk, err := decode(raw)
	if err != nil {
		return nil, err
	}
	if d, ok := pk.(*v2193.DisconnectPacket); ok {
		_ = c.Close()
		return nil, DisconnectError(d.Message)
	}
	return pk, nil
}

// WritePacket adds a packet to the batch Flush sends.
func (c *Conn) WritePacket(pk proto.Packet) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.w.B = bedrock.AppendHeader(c.w.B[:0], bedrock.Header{ID: pk.ID()})
	pk.Encode(&c.w)
	return c.b.WritePacket(c.w.B)
}

// Flush sends the packets written since the last flush as one batch.
func (c *Conn) Flush() error { return c.b.Flush() }

// send writes a packet and flushes it.
func (c *Conn) send(pk proto.Packet) error {
	if err := c.WritePacket(pk); err != nil {
		return err
	}
	return c.Flush()
}

// Close closes the connection.
func (c *Conn) Close() error { return c.nn.Close() }
