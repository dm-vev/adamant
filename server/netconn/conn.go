// Package netconn runs the Bedrock connections of the server over
// NetherNet: it logs a client in, hands it the resource packs and starts its
// game, then carries the packets of its session. Packets are coded with
// the generated codecs of protocol 2193 and batched by package bedrock of
// the net module. A connection has no goroutine of its own once it logged in: its
// session reads and writes on its own goroutines, and a batch goes out when
// the session flushes.
package netconn

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
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
	"github.com/dm-vev/adamant/net/bedrock/login"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// DisconnectError is returned by ReadPacket once the client disconnected,
// with the message it gave.
type DisconnectError string

func (e DisconnectError) Error() string { return "netconn: client disconnected: " + string(e) }

// Conn is the connection of a client that logged in. It implements the
// connection a session of the server needs.
type Conn struct {
	l  *Listener
	nn *nethernet.Conn
	b  *bedrock.Conn

	identity     login.Identity
	client       login.ClientData
	cacheEnabled atomic.Bool
	chunkRadius  atomic.Int32
	shieldID     atomic.Int32
	runtimeID    uint64

	// deferred holds packets read during the login and the spawn that the
	// session reads first.
	deferred [][]byte
	// limits holds the buckets of packetLimits, used by the reader alone.
	limits [len(packetLimits)]limitBucket

	writeMu sync.Mutex
	// w encodes the packets written.
	w wire.Writer

	closeOnce sync.Once
	connected time.Time
}

func newConn(l *Listener, nn *nethernet.Conn) *Conn {
	return &Conn{
		l:         l,
		nn:        nn,
		b:         bedrock.NewConn(nn, l.batches),
		connected: time.Now(),
	}
}

// Stats are the state of the connection of a client, as a netstat shows
// it.
type Stats struct {
	nethernet.Stats
	// Protocol is the protocol the client speaks.
	Protocol int32
	// Connected is when the connection opened.
	Connected time.Time
}

// Stats returns the state of the connection.
func (c *Conn) Stats() Stats {
	st := Stats{Stats: c.nn.Stats(), Protocol: v2193.Protocol, Connected: c.connected}
	st.Relayed = c.l.nn.Relayed(st.Remote)
	return st
}

// IdentityData returns the identity the client logged in with.
func (c *Conn) IdentityData() login.Identity { return c.identity }

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
func (c *Conn) WritePacket(pk proto.Packet) error {
	if st, ok := pk.(Static); ok {
		msg, err := c.l.static(st, c.shieldID.Load())
		if err != nil {
			return err
		}
		return c.b.SendBatch(msg)
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.w.B = bedrock.AppendHeader(c.w.B[:0], bedrock.Header{ID: pk.ID()})
	pk.Encode(&c.w)
	err := c.b.WritePacket(c.w.B)
	if cap(c.w.B) > keptBuffer {
		// Large packets are rare: the buffer is not kept for them.
		c.w.B = nil
	}
	return err
}

// keptBuffer bounds the buffer a Conn keeps to encode packets.
const keptBuffer = 64 << 10

// Flush sends the packets written since the last flush as one batch.
func (c *Conn) Flush() error { return c.b.Flush() }

// ReadPacket returns the next packet of the client. A packet over the
// bucket of its kind is dropped.
func (c *Conn) ReadPacket() (pk proto.Packet, err error) {
	defer c.recoverRead(&err)
	raw, err := c.next()
	for err == nil && c.overLimit(raw) {
		raw, err = c.next()
	}
	if err != nil {
		return nil, err
	}
	if pk, err = DecodePacket(raw); err != nil {
		undecodable.Inc()
		_ = c.Close()
		return nil, err
	}
	if d, ok := pk.(*v2193.DisconnectPacket); ok {
		_ = c.Close()
		return nil, DisconnectError(d.Message)
	}
	return pk, nil
}

// errStopped stops the reading of a batch once a packet of it ended the
// connection.
var errStopped = errors.New("netconn: stopped")

// Receive has each packet the client sends from now on handed to handle,
// in order, on the goroutine of the transport that received it, instead
// of being queued for ReadPacket; the packets read during the spawn go
// first. closed is called once the connection closed: the client left,
// sent too much or sent a packet that does not decode. Neither may block.
func (c *Conn) Receive(handle func(pk proto.Packet), closed func(err error)) {
	var once sync.Once
	end := func(err error) { once.Do(func() { closed(err) }) }
	deliver := func(raw []byte) (err error) {
		defer c.recoverRead(&err)
		if c.overLimit(raw) {
			return nil
		}
		pk, err := DecodePacket(raw)
		if err != nil {
			undecodable.Inc()
			_ = c.Close()
			end(err)
			return errStopped
		}
		if d, ok := pk.(*v2193.DisconnectPacket); ok {
			_ = c.Close()
			end(DisconnectError(d.Message))
			return errStopped
		}
		handle(pk)
		return nil
	}
	read := append(c.deferred, c.b.TakePending()...)
	c.deferred = nil
	for _, raw := range read {
		if err := deliver(raw); err != nil {
			if err != errStopped {
				end(err)
			}
			return
		}
	}
	c.nn.SetReceiver(func(msg []byte) {
		if err := c.b.HandleBatch(msg, deliver); err != nil {
			_ = c.Close()
			if err != errStopped {
				end(err)
			}
		}
	})
	c.nn.OnClose(end)
}

// packetLimits are the buckets BDS keeps for packets a client has no need
// to send often (packetlimitconfig.json of BDS 1.26.52): each packet fills
// the bucket of its group by one, which drains at rate a second; a packet
// that would overfill it is dropped.
var packetLimits = [...]struct {
	ids        []uint32
	rate, size float64
}{
	{[]uint32{packet.IDRequestNetworkSettings, packet.IDClientToServerHandshake}, 0.0013, 1},
	{[]uint32{packet.IDText, packet.IDBookEdit}, 10, 50},
	{[]uint32{packet.IDPlayerSkin}, 10, 50},
	{[]uint32{packet.IDPositionTrackingDBClientRequest}, 20, 60},
}

// limitGroup maps a packet ID to its group in packetLimits plus one, zero
// for none.
var limitGroup = func() (m [1024]uint8) {
	for i, l := range packetLimits {
		for _, id := range l.ids {
			m[id] = uint8(i + 1)
		}
	}
	return m
}()

// limitBucket is the level of the bucket of a group.
type limitBucket struct {
	level float64
	last  time.Time
}

// overLimit reports a packet that would overfill the bucket of its group,
// filling it otherwise.
func (c *Conn) overLimit(raw []byte) bool {
	id, n := binary.Uvarint(raw)
	if n <= 0 {
		return false
	}
	g := limitGroup[id&0x3ff]
	if g == 0 {
		return false
	}
	l, b := &packetLimits[g-1], &c.limits[g-1]
	now := time.Now()
	if !b.last.IsZero() {
		b.level = max(0, b.level-now.Sub(b.last).Seconds()*l.rate)
	}
	b.last = now
	if b.level+1 > l.size {
		return true
	}
	b.level++
	return false
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

// maxList bounds the elements of a list a client sends, as gophertunnel
// bounds them for a server. A string or a byte slice takes the bytes of
// its packet and no more, so the packet alone bounds it: a login carries a
// skin of any size the game makes.
const (
	maxList   = 1024
	maxString = math.MaxInt32
)

// DecodePacket decodes a packet a client sent, header first, with the
// codecs of protocol 2193 and the limits a server keeps against a client.
func DecodePacket(raw []byte) (pk proto.Packet, err error) {
	h, fields, err := bedrock.ReadHeader(raw)
	if err != nil {
		return nil, err
	}
	if pk = v2193.New(h.ID); pk == nil {
		return nil, errUnknownPacket(h.ID)
	}
	defer func() {
		// The codecs fail softly; a panic is a bug of a hand-written one,
		// which must not take the server down.
		if v := recover(); v != nil {
			pk, err = nil, fmt.Errorf("netconn: decode %T: %v", pk, v)
		}
	}()
	r := wire.NewReader(fields)
	r.MaxList, r.MaxString = maxList, maxString
	pk.Decode(r)
	if err := r.Err(); err != nil {
		return nil, fmt.Errorf("netconn: decode %T: %w", pk, err)
	}
	if r.Len() != 0 {
		return nil, fmt.Errorf("netconn: decode %T: %d bytes left", pk, r.Len())
	}
	return pk, nil
}

// recoverRead turns a panic reading what the client sent, a bug no client
// may use to take the server down, into an error that closes the
// connection.
func (c *Conn) recoverRead(err *error) {
	if v := recover(); v != nil {
		_ = c.Close()
		*err = fmt.Errorf("netconn: read: panic: %v", v)
	}
}

// read returns the next packet with one of the IDs passed. Other packets
// are deferred for the session; a Disconnect ends the connection.
func (c *Conn) read(ids ...uint32) (pk proto.Packet, err error) {
	defer c.recoverRead(&err)
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
			pk, err := DecodePacket(raw)
			if err != nil {
				return nil, err
			}
			return nil, DisconnectError(pk.(*v2193.DisconnectPacket).Message)
		}
		for _, id := range ids {
			if h.ID == id {
				return DecodePacket(raw)
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
func (c *Conn) StartGameContext(ctx context.Context, data GameData) error {
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
		_ = c.WritePacket(&v2193.DimensionDataPacket{Definitions: data.Dimensions})
	}
	_ = c.WritePacket(&v2193.JigsawStructureDataPacket{StructureData: map[string]any{
		"processors":     make([]map[string]any, 0),
		"template_pools": make([]map[string]any, 0),
		"jigsaws":        make([]map[string]any, 0),
		"structure_sets": make([]map[string]any, 0),
	}})
	_ = c.WritePacket(&v2193.VoxelShapesPacket{})
	_ = c.WritePacket(startGame(data))
	_ = c.WritePacket(&v2193.ItemRegistryPacket{Items: data.Items})
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
		case *v2193.RequestChunkRadiusPacket:
			if pk.ChunkRadius < 1 {
				return fmt.Errorf("netconn: chunk radius %d", pk.ChunkRadius)
			}
			radius := pk.ChunkRadius
			if data.ChunkRadius != 0 {
				radius = data.ChunkRadius
			}
			c.chunkRadius.Store(pk.ChunkRadius)
			_ = c.WritePacket(&v2193.ChunkRadiusUpdatedPacket{ChunkRadius: radius})
			_ = c.WritePacket(&v2193.PlayStatusPacket{Status: packet.PlayStatusPlayerSpawn})
			_ = c.WritePacket(&v2193.CreativeContentPacket{})
			if err := c.Flush(); err != nil {
				return err
			}
		case *v2193.SetLocalPlayerAsInitialisedPacket:
			if pk.EntityRuntimeID != c.runtimeID {
				return fmt.Errorf("netconn: client initialised runtime ID %d, not %d", pk.EntityRuntimeID, c.runtimeID)
			}
			return nil
		}
	}
}

// startGame returns the StartGame packet of the game data, filled as
// gophertunnel fills it.
func startGame(data GameData) *v2193.StartGamePacket {
	return &v2193.StartGamePacket{
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
		GameVersion:                  v2193.Version,
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
