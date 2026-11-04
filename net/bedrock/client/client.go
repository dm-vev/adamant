// Package client joins Bedrock servers over NetherNet as the game does,
// as a client of a protocol it is configured with, 2193 by default: it logs
// in offline or with a token of an account, takes no resource packs,
// spawns, and then reads and writes packets. Its packets are those of the
// model, package v2225, which a client of another protocol converts to and
// from its own as package codec does.
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
	"github.com/dm-vev/adamant/net/bedrock/proto/codec"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
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
	// Protocol is the protocol the client speaks; nil means codec.V2193.
	Protocol *codec.Codec
}

// Conn is a client spawned in a server.
type Conn struct {
	nn    *nethernet.Conn
	b     *bedrock.Conn
	proto *codec.Codec
	game  mc.StartGamePacket
	// converted holds the packets of the model past the first that a
	// packet read converted to, which ReadPacket returns next.
	converted []proto.Packet

	// deferred holds the packets read during the spawn, read first.
	deferred [][]byte
	// items is the item registry the server sent during the spawn.
	items []mc.ItemEntry

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
	if cfg.Protocol == nil {
		cfg.Protocol = codec.V2193
	}
	data := login.DefaultClientData(serverAddress(address), cfg.Identity.DisplayName, cfg.Protocol.Version)
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
	c := &Conn{nn: nn, b: bedrock.NewConn(nn, cfg.Batches), proto: cfg.Protocol}
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
	if err := c.send(&mc.RequestNetworkSettingsPacket{ClientProtocol: c.proto.Number}); err != nil {
		return err
	}
	pk, _, err := c.next()
	if err != nil {
		return err
	}
	settings, ok := pk.(*mc.NetworkSettingsPacket)
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
	if err := c.send(&mc.LoginPacket{ClientProtocol: c.proto.Number, ConnectionRequest: request}); err != nil {
		return err
	}
	started := false
	for {
		pk, raw, err := c.next()
		if err != nil {
			return err
		}
		switch pk := pk.(type) {
		case *mc.PlayStatusPacket:
			switch pk.Status {
			case loginSuccess:
				continue
			case playerSpawn:
				if !started {
					return errors.New("client: spawned before StartGame")
				}
				return c.send(&mc.SetLocalPlayerAsInitialisedPacket{EntityRuntimeID: c.game.EntityRuntimeID})
			default:
				return fmt.Errorf("client: login failed with status %d", pk.Status)
			}
		case *mc.ServerToClientHandshakePacket:
			// BDS asks for the handshake that turns encryption on over RakNet
			// even over NetherNet, which encrypts the connection itself: the
			// answer turns nothing on.
			err = c.send(&mc.ClientToServerHandshakePacket{})
		case *mc.ResourcePacksInfoPacket:
			err = c.send(&mc.ResourcePackClientResponsePacket{Response: allPacksDownloaded})
		case *mc.ResourcePackStackPacket:
			err = c.send(&mc.ResourcePackClientResponsePacket{Response: packsCompleted})
		case *mc.ItemRegistryPacket:
			c.items = pk.Items
			// Shields carry one more field, so their ID must be known.
			for _, item := range pk.Items {
				if item.Name == "minecraft:shield" {
					codec.SetShieldID(c.proto, int32(item.RuntimeID))
				}
			}
			if started {
				c.deferred = append(c.deferred, raw)
			}
		case *mc.StartGamePacket:
			c.game, started = *pk, true
			err = c.send(&mc.RequestChunkRadiusPacket{ChunkRadius: cfg.ChunkRadius, MaxChunkRadius: uint8(min(cfg.ChunkRadius, 255))})
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

// next reads and decodes the next packet of the login, failing on a
// Disconnect or on a packet that does not decode. The packets of the login
// have one layout in every protocol, but StartGame, which is one packet of
// the model in every protocol too.
func (c *Conn) next() (proto.Packet, []byte, error) {
	raw, err := c.b.ReadPacket()
	if err != nil {
		return nil, nil, err
	}
	pks, err := c.decode(raw)
	if err != nil {
		return nil, nil, err
	}
	if len(pks) != 1 {
		return nil, nil, fmt.Errorf("client: a packet of the login is %d packets of the model", len(pks))
	}
	if d, ok := pks[0].(*mc.DisconnectPacket); ok {
		return nil, nil, DisconnectError(d.Message)
	}
	return pks[0], raw, nil
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

// decode decodes a packet of the protocol of the client as the packets of
// the model that carry it, none if the model has no such packet.
func (c *Conn) decode(raw []byte) ([]proto.Packet, error) {
	h, body, err := bedrock.ReadHeader(raw)
	if err != nil {
		return nil, err
	}
	r := wire.NewReader(body)
	pks := c.proto.Decode(h.ID, r)
	if pks == nil && r.Err() == nil {
		return nil, &DecodeError{ID: h.ID, Err: errors.New("unknown packet")}
	}
	if r.Err() == nil && r.Len() != 0 {
		r.Failf("%d bytes left", r.Len())
	}
	if r.Err() != nil {
		return nil, &DecodeError{ID: h.ID, Err: r.Err()}
	}
	return pks, nil
}

// StartGame returns the StartGame packet the server spawned the player
// with.
func (c *Conn) StartGame() *mc.StartGamePacket { return &c.game }

// Items returns the item registry the server sent during the spawn.
func (c *Conn) Items() []mc.ItemEntry { return c.items }

// ReadPacket returns the next packet of the server, as a packet of the
// model. A *DecodeError leaves the connection usable; a Disconnect ends it
// with a DisconnectError. A packet the model has no form of is skipped.
func (c *Conn) ReadPacket() (proto.Packet, error) {
	for len(c.converted) == 0 {
		var raw []byte
		if len(c.deferred) > 0 {
			raw, c.deferred = c.deferred[0], c.deferred[1:]
		} else {
			var err error
			if raw, err = c.b.ReadPacket(); err != nil {
				return nil, err
			}
		}
		pks, err := c.decode(raw)
		if err != nil {
			return nil, err
		}
		c.converted = append(c.converted, pks...)
	}
	pk := c.converted[0]
	c.converted = c.converted[1:]
	if d, ok := pk.(*mc.DisconnectPacket); ok {
		_ = c.Close()
		return nil, DisconnectError(d.Message)
	}
	return pk, nil
}

// Protocol returns the protocol the client speaks.
func (c *Conn) Protocol() *codec.Codec { return c.proto }

// WritePacket adds a packet of the model to the batch Flush sends, as the
// packets of the protocol of the client that carry it.
func (c *Conn) WritePacket(pk proto.Packet) error {
	return c.proto.Encode(pk, pk, c.write)
}

// write adds a packet of the protocol of the client to the batch.
func (c *Conn) write(pk proto.Packet) error {
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
