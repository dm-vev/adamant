package main

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

// conn is a bot connection. It joins as net/bedrock/client does, but keeps
// the NetherNet connection, whose counters give the bytes on the wire, and
// hands out packets undecoded, so that a bot decodes only the packets it
// looks at and costs the host as little as it can.
type conn struct {
	nn *nethernet.Conn
	b  *bedrock.Conn

	game  v2193.StartGamePacket
	items []v2193.ItemEntry
	// deferred holds the packets read during the spawn, read first.
	deferred [][]byte

	writeMu sync.Mutex
	w       wire.Writer
}

// dial joins the server at address and returns once the player spawned.
func dial(ctx context.Context, address, name string, radius int32) (*conn, error) {
	key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		return nil, err
	}
	id := login.Identity{DisplayName: name, Identity: login.NewUUID()}
	host := address
	if u, err := url.Parse(address); err == nil && u.Host != "" {
		host = u.Host
	}
	data := login.DefaultClientData(host, name, v2193.Version)
	request, err := login.EncodeOffline(id, data, key, time.Now())
	if err != nil {
		return nil, err
	}
	nn, err := nethernet.Dialer{Key: key}.DialContext(ctx, address)
	if err != nil {
		return nil, err
	}
	c := &conn{nn: nn, b: bedrock.NewConn(nn, bedrock.Config{})}
	stop := context.AfterFunc(ctx, func() { _ = nn.SetReadDeadline(time.Now()) })
	err = c.join(request, radius)
	if !stop() || err != nil {
		_ = nn.Close()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	return c, nil
}

// join logs in and spawns, as net/bedrock/client does.
func (c *conn) join(request []byte, radius int32) error {
	if err := c.send(&v2193.RequestNetworkSettingsPacket{ClientProtocol: v2193.Protocol}); err != nil {
		return err
	}
	pk, _, err := c.next()
	if err != nil {
		return err
	}
	settings, ok := pk.(*v2193.NetworkSettingsPacket)
	if !ok {
		return fmt.Errorf("%T instead of NetworkSettings", pk)
	}
	switch settings.CompressionAlgorithm {
	case bedrock.AlgorithmFlate:
		c.b.EnableCompression(bedrock.Flate{})
	case bedrock.AlgorithmNone:
		c.b.EnableCompression(bedrock.None{})
	default:
		return fmt.Errorf("compression %d is not supported", settings.CompressionAlgorithm)
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
			case 0:
				continue
			case 3:
				if !started {
					return errors.New("spawned before StartGame")
				}
				return c.send(&v2193.SetLocalPlayerAsInitialisedPacket{EntityRuntimeID: c.game.EntityRuntimeID})
			default:
				return fmt.Errorf("login failed with status %d", pk.Status)
			}
		case *v2193.ServerToClientHandshakePacket:
			err = c.send(&v2193.ClientToServerHandshakePacket{})
		case *v2193.ResourcePacksInfoPacket:
			err = c.send(&v2193.ResourcePackClientResponsePacket{Response: 3})
		case *v2193.ResourcePackStackPacket:
			err = c.send(&v2193.ResourcePackClientResponsePacket{Response: 4})
		case *v2193.ItemRegistryPacket:
			c.items = pk.Items
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
			err = c.send(&v2193.RequestChunkRadiusPacket{ChunkRadius: radius, MaxChunkRadius: uint8(min(radius, 255))})
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

// next reads the next packet of the login and decodes it. A packet that
// does not decode is returned with a nil packet: the bot does not need it.
func (c *conn) next() (proto.Packet, []byte, error) {
	raw, err := c.b.ReadPacket()
	if err != nil {
		return nil, nil, err
	}
	id, body, err := header(raw)
	if err != nil {
		return nil, nil, err
	}
	pk, err := decode(id, body)
	if err != nil {
		return nil, raw, nil
	}
	if d, ok := pk.(*v2193.DisconnectPacket); ok {
		return nil, nil, fmt.Errorf("disconnected: %s", d.Message)
	}
	return pk, raw, nil
}

// header returns the ID and the body of a packet.
func header(raw []byte) (uint32, []byte, error) {
	h, body, err := bedrock.ReadHeader(raw)
	if err != nil {
		return 0, nil, err
	}
	return h.ID, body, nil
}

// decode decodes the body of a packet of an ID.
func decode(id uint32, body []byte) (proto.Packet, error) {
	pk := v2193.New(id)
	if pk == nil {
		return nil, fmt.Errorf("unknown packet %d", id)
	}
	r := wire.NewReader(body)
	pk.Decode(r)
	if r.Err() == nil && r.Len() != 0 {
		r.Failf("%d bytes left", r.Len())
	}
	if r.Err() != nil {
		return nil, fmt.Errorf("decode packet %d: %w", id, r.Err())
	}
	return pk, nil
}

// readRaw returns the next packet of the server, undecoded.
func (c *conn) readRaw() ([]byte, error) {
	if len(c.deferred) > 0 {
		raw := c.deferred[0]
		c.deferred = c.deferred[1:]
		return raw, nil
	}
	return c.b.ReadPacket()
}

// write adds a packet to the batch flush sends.
func (c *conn) write(pk proto.Packet) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.w.B = bedrock.AppendHeader(c.w.B[:0], bedrock.Header{ID: pk.ID()})
	pk.Encode(&c.w)
	return c.b.WritePacket(c.w.B)
}

// flush sends the packets written since the last flush.
func (c *conn) flush() error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.b.Flush()
}

func (c *conn) send(pk proto.Packet) error {
	if err := c.write(pk); err != nil {
		return err
	}
	return c.flush()
}

// wireBytes returns the bytes received on the reliable channel, as they
// came over the wire, compressed.
func (c *conn) wireBytes() uint64 { return c.nn.Stats().BytesReceived }

func (c *conn) close() error { return c.nn.Close() }
