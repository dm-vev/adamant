// Package netconn runs the Bedrock connections of the server over
// NetherNet: it logs a client in, hands it the resource packs and starts its
// game, then carries the packets of its session. Packets are encoded with
// the codecs of gophertunnel and batched by package bedrock of the net
// module. A connection has no goroutine of its own once it logged in: its
// session reads and writes on its own goroutines, and a batch goes out when
// the session flushes.
package netconn

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dm-vev/adamant/net/bedrock"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/dm-vev/adamant/net/bedrock/wire"
	"github.com/dm-vev/adamant/net/nethernet"
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// DisconnectError is returned by ReadPacket once the client disconnected,
// with the message it gave.
type DisconnectError string

func (e DisconnectError) Error() string { return "netconn: client disconnected: " + string(e) }

// Conn is the connection of a client that logged in. It implements the
// connection a session of the server needs.
type Conn struct {
	l   *Listener
	nn  *nethernet.Conn
	b   *bedrock.Conn
	ids packet.Pool

	identity     login.IdentityData
	client       login.ClientData
	cacheEnabled atomic.Bool
	chunkRadius  atomic.Int32
	shieldID     atomic.Int32
	runtimeID    uint64

	// deferred holds packets read during the login and the spawn that the
	// session reads first.
	deferred [][]byte

	writeMu sync.Mutex
	buf     bytes.Buffer
	// w encodes packets into buf for the shield ID wShield.
	w       *protocol.Writer
	wShield int32
	// pw encodes the packets of the generated codecs.
	pw wire.Writer

	closeOnce sync.Once
}

func newConn(l *Listener, nn *nethernet.Conn) *Conn {
	return &Conn{
		l:   l,
		nn:  nn,
		b:   bedrock.NewConn(nn, l.batches),
		ids: packet.NewClientPool(),
	}
}

// IdentityData returns the identity the client logged in with.
func (c *Conn) IdentityData() login.IdentityData { return c.identity }

// ClientData returns the client data of the login: skin, locale and such.
func (c *Conn) ClientData() login.ClientData { return c.client }

// ClientCacheEnabled reports whether the client caches chunks.
func (c *Conn) ClientCacheEnabled() bool { return c.cacheEnabled.Load() }

// ChunkRadius returns the chunk radius the client asked for.
func (c *Conn) ChunkRadius() int { return int(c.chunkRadius.Load()) }

// Latency returns half the round trip time of the transport.
func (c *Conn) Latency() time.Duration { return c.nn.Latency() }

// RemoteAddr returns the address of the client.
func (c *Conn) RemoteAddr() net.Addr { return c.nn.RemoteAddr() }

// WritePacket adds a packet to the batch sent at the next Flush. A Static
// goes in a batch of its own, encoded once for every connection.
func (c *Conn) WritePacket(pk packet.Packet) error {
	if st, ok := pk.(Static); ok {
		msg, err := c.l.static(st, c.shieldID.Load())
		if err != nil {
			return err
		}
		return c.b.SendBatch(msg)
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.buf.Reset()
	c.buf.Write(bedrock.AppendHeader(c.buf.AvailableBuffer(), bedrock.Header{ID: pk.ID()}))
	if shield := c.shieldID.Load(); c.w == nil || c.wShield != shield {
		// One writer serves every packet: making one for each was most of
		// what a server allocated to send them.
		c.w, c.wShield = protocol.NewWriter(&c.buf, shield), shield
	}
	pk.Marshal(c.w)
	err := c.b.WritePacket(c.buf.Bytes())
	if c.buf.Cap() > keptBuffer {
		// Large packets are rare: the buffer is not kept for them.
		c.buf = bytes.Buffer{}
	}
	return err
}

// WriteProto adds a packet of the generated codecs to the batch sent at the
// next Flush.
func (c *Conn) WriteProto(pk proto.Packet) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.pw.B = bedrock.AppendHeader(c.pw.B[:0], bedrock.Header{ID: pk.ID()})
	pk.Encode(&c.pw)
	err := c.b.WritePacket(c.pw.B)
	if cap(c.pw.B) > keptBuffer {
		c.pw.B = nil
	}
	return err
}

// keptBuffer bounds the buffer a Conn keeps to encode packets.
const keptBuffer = 64 << 10

// Flush sends the packets written since the last flush as one batch.
func (c *Conn) Flush() error { return c.b.Flush() }

// ReadPacket returns the next packet of the client.
func (c *Conn) ReadPacket() (packet.Packet, error) {
	raw, err := c.next()
	if err != nil {
		return nil, err
	}
	pk, err := c.decode(raw)
	if err != nil {
		_ = c.Close()
		return nil, err
	}
	if d, ok := pk.(*packet.Disconnect); ok {
		_ = c.Close()
		return nil, DisconnectError(d.Message)
	}
	return pk, nil
}

// next returns the next packet as read, the deferred ones first.
func (c *Conn) next() ([]byte, error) {
	if len(c.deferred) > 0 {
		raw := c.deferred[0]
		c.deferred = c.deferred[1:]
		return raw, nil
	}
	return c.b.ReadPacket()
}

// errUnknownPacket is returned for a packet the codecs do not know: the
// client is dropped, as gophertunnel drops it.
type errUnknownPacket uint32

func (e errUnknownPacket) Error() string { return fmt.Sprintf("netconn: unknown packet %d", uint32(e)) }

// decode decodes a packet with the codecs of gophertunnel, with the limits
// a server keeps against a client.
func (c *Conn) decode(raw []byte) (pk packet.Packet, err error) {
	h, fields, err := bedrock.ReadHeader(raw)
	if err != nil {
		return nil, err
	}
	newPacket, ok := c.ids[h.ID]
	if !ok {
		return nil, errUnknownPacket(h.ID)
	}
	pk = newPacket()
	r := bytes.NewBuffer(fields)
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("netconn: decode %T: %v", pk, v)
		}
	}()
	pk.Marshal(protocol.NewReader(r, c.shieldID.Load(), true))
	if r.Len() != 0 {
		return nil, fmt.Errorf("netconn: decode %T: %d bytes left", pk, r.Len())
	}
	return pk, nil
}

// read returns the next packet with one of the IDs passed. Other packets
// are deferred for the session; a Disconnect ends the connection.
func (c *Conn) read(ids ...uint32) (packet.Packet, error) {
	for {
		raw, err := c.b.ReadPacket()
		if err != nil {
			return nil, err
		}
		h, _, err := bedrock.ReadHeader(raw)
		if err != nil {
			return nil, err
		}
		if h.ID == packet.IDDisconnect {
			pk, err := c.decode(raw)
			if err != nil {
				return nil, err
			}
			return nil, DisconnectError(pk.(*packet.Disconnect).Message)
		}
		for _, id := range ids {
			if h.ID == id {
				return c.decode(raw)
			}
		}
		if len(c.deferred) == maxDeferred {
			return nil, errors.New("netconn: client sends too much before it spawned")
		}
		c.deferred = append(c.deferred, raw)
	}
}

// maxDeferred bounds the packets kept for the session before the spawn.
const maxDeferred = 256

// StartGameContext starts the game of the client: the world it spawns in,
// the items of the server, and its spawn once it asked for a chunk radius.
// It returns once the client is in the world.
func (c *Conn) StartGameContext(ctx context.Context, data minecraft.GameData) error {
	if data.WorldName == "" {
		data.WorldName = c.l.status().ServerName
	}
	for _, item := range data.Items {
		if item.Name == "minecraft:shield" {
			c.shieldID.Store(int32(item.RuntimeID))
			// Items of the generated codecs find the shield by a process-wide ID.
			v2193.SetShieldID(int32(item.RuntimeID))
		}
	}
	c.runtimeID = data.EntityRuntimeID
	if len(data.Dimensions) > 0 {
		_ = c.WritePacket(&packet.DimensionData{Definitions: data.Dimensions})
	}
	_ = c.WritePacket(&packet.JigsawStructureData{StructureData: map[string]any{
		"processors":     make([]map[string]any, 0),
		"template_pools": make([]map[string]any, 0),
		"jigsaws":        make([]map[string]any, 0),
		"structure_sets": make([]map[string]any, 0),
	}})
	_ = c.WritePacket(&packet.VoxelShapes{})
	_ = c.WritePacket(startGame(data))
	_ = c.WritePacket(&packet.ItemRegistry{Items: data.Items})
	if err := c.Flush(); err != nil {
		return err
	}

	// A cancelled context ends the wait for the client.
	stop := context.AfterFunc(ctx, func() { _ = c.nn.SetReadDeadline(time.Now()) })
	defer func() {
		stop()
		_ = c.nn.SetReadDeadline(time.Time{})
	}()
	for {
		pk, err := c.read(packet.IDRequestChunkRadius, packet.IDSetLocalPlayerAsInitialised)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		switch pk := pk.(type) {
		case *packet.RequestChunkRadius:
			if pk.ChunkRadius < 1 {
				return fmt.Errorf("netconn: chunk radius %d", pk.ChunkRadius)
			}
			radius := pk.ChunkRadius
			if data.ChunkRadius != 0 {
				radius = data.ChunkRadius
			}
			c.chunkRadius.Store(pk.ChunkRadius)
			_ = c.WritePacket(&packet.ChunkRadiusUpdated{ChunkRadius: radius})
			_ = c.WritePacket(&packet.PlayStatus{Status: packet.PlayStatusPlayerSpawn})
			_ = c.WritePacket(&packet.CreativeContent{})
			if err := c.Flush(); err != nil {
				return err
			}
		case *packet.SetLocalPlayerAsInitialised:
			if pk.EntityRuntimeID != c.runtimeID {
				return fmt.Errorf("netconn: client initialised runtime ID %d, not %d", pk.EntityRuntimeID, c.runtimeID)
			}
			return nil
		}
	}
}

// startGame returns the StartGame packet of the game data, filled as
// gophertunnel fills it.
func startGame(data minecraft.GameData) *packet.StartGame {
	return &packet.StartGame{
		Difficulty:                   data.Difficulty,
		EntityUniqueID:               data.EntityUniqueID,
		EntityRuntimeID:              data.EntityRuntimeID,
		PlayerGameMode:               data.PlayerGameMode,
		PlayerPosition:               data.PlayerPosition,
		Pitch:                        data.Pitch,
		Yaw:                          data.Yaw,
		WorldSeed:                    data.WorldSeed,
		Dimension:                    data.Dimension,
		WorldSpawn:                   data.WorldSpawn,
		EditorWorldType:              data.EditorWorldType,
		CreatedInEditor:              data.CreatedInEditor,
		ExportedFromEditor:           data.ExportedFromEditor,
		PersonaDisabled:              data.PersonaDisabled,
		CustomSkinsDisabled:          data.CustomSkinsDisabled,
		EmoteChatMuted:               data.EmoteChatMuted,
		GameRules:                    data.GameRules,
		Time:                         data.Time,
		DayCycleLockTime:             data.DayCycleLockTime,
		Blocks:                       data.CustomBlocks,
		AchievementsDisabled:         true,
		Generator:                    1,
		EducationFeaturesEnabled:     true,
		MultiPlayerGame:              true,
		MultiPlayerCorrelationID:     uuid.NewString(),
		CommandsEnabled:              true,
		WorldName:                    data.WorldName,
		LANBroadcastEnabled:          true,
		PlayerMovementSettings:       data.PlayerMovementSettings,
		WorldGameMode:                data.WorldGameMode,
		Hardcore:                     data.Hardcore,
		XBLBroadcastMode:             data.XBLBroadcastMode,
		ServerAuthoritativeInventory: data.ServerAuthoritativeInventory,
		PlayerPermissions:            data.PlayerPermissions,
		Experiments:                  data.Experiments,
		ClientSideGeneration:         data.ClientSideGeneration,
		ChatRestrictionLevel:         data.ChatRestrictionLevel,
		DisablePlayerInteractions:    data.DisablePlayerInteractions,
		BaseGameVersion:              data.BaseGameVersion,
		GameVersion:                  protocol.CurrentVersion,
		UseBlockNetworkIDHashes:      data.UseBlockNetworkIDHashes,
		PropertyData:                 data.PropertyData,
	}
}

// Close closes the connection. Packets flushed before still reach the
// client.
func (c *Conn) Close() error {
	c.closeOnce.Do(func() {
		_ = c.nn.Close()
		c.l.left()
	})
	return nil
}
