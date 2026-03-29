package session

import (
	"context"
	"io"
	"log/slog"
	"net"
	"strconv"
	"testing"
	"time"

	_ "github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/sandertv/gophertunnel/minecraft"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func BenchmarkEncodeChunkBlockEntities(b *testing.B) {
	for _, count := range []int{0, 32, 256} {
		blockEntities := benchmarkBlockEntities(b, count)
		b.Run(benchmarkName(count), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = encodeChunkBlockEntities(blockEntities)
			}
		})
	}
}

func BenchmarkEncodeSubChunkBlockEntities(b *testing.B) {
	for _, count := range []int{0, 16, 64} {
		col, ind := benchmarkColumnWithBlockEntities(b, count)
		b.Run(benchmarkName(count), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = encodeSubChunkBlockEntities(col, ind)
			}
		})
	}
}

func BenchmarkSubChunkEntry(b *testing.B) {
	for _, cacheEnabled := range []bool{false, true} {
		col, ind := benchmarkColumnWithBlockEntities(b, 32)
		session := benchmarkSession(cacheEnabled)
		name := "cache_off"
		if cacheEnabled {
			name = "cache_on"
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				transaction := make(map[uint64]struct{}, 1)
				_ = session.subChunkEntry(protocol.SubChunkOffset{}, ind, col, transaction)
			}
		})
	}
}

func BenchmarkNetworkBiomePayload(b *testing.B) {
	baseCol, _ := benchmarkColumnWithBlockEntities(b, 32)
	b.Run("cold", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			col := &world.Column{Chunk: baseCol.Chunk, BlockEntities: baseCol.BlockEntities}
			_ = networkBiomePayload(col)
		}
	})
	b.Run("warm", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = networkBiomePayload(baseCol)
		}
	})
}

func BenchmarkNetworkSubChunkPayload(b *testing.B) {
	baseCol, ind := benchmarkColumnWithBlockEntities(b, 32)
	b.Run("cold", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			col := &world.Column{Chunk: baseCol.Chunk, BlockEntities: baseCol.BlockEntities}
			_ = networkSubChunkPayload(col, ind)
		}
	})
	b.Run("warm", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = networkSubChunkPayload(baseCol, ind)
		}
	})
}

func benchmarkBlockEntities(b *testing.B, count int) map[cube.Pos]world.Block {
	b.Helper()

	beaconRID, ok := chunk.StateToRuntimeID("minecraft:beacon", nil)
	if !ok {
		b.Fatal("beacon runtime ID not found")
	}
	beacon, ok := world.BlockByRuntimeID(beaconRID)
	if !ok {
		b.Fatal("beacon block not found")
	}

	blockEntities := make(map[cube.Pos]world.Block, count)
	for i := 0; i < count; i++ {
		x := i & 15
		z := (i >> 4) & 15
		y := 64 + ((i >> 8) & 15)
		blockEntities[cube.Pos{x, y, z}] = beacon
	}
	return blockEntities
}

func benchmarkColumnWithBlockEntities(b *testing.B, count int) (*world.Column, int16) {
	b.Helper()

	airRID, ok := chunk.StateToRuntimeID("minecraft:air", nil)
	if !ok {
		b.Fatal("air runtime ID not found")
	}
	stoneRID, ok := chunk.StateToRuntimeID("minecraft:stone", nil)
	if !ok {
		b.Fatal("stone runtime ID not found")
	}
	beaconRID, ok := chunk.StateToRuntimeID("minecraft:beacon", nil)
	if !ok {
		b.Fatal("beacon runtime ID not found")
	}

	c := chunk.New(airRID, cube.Range{0, 255})
	for x := uint8(0); x < 16; x++ {
		for z := uint8(0); z < 16; z++ {
			for y := int16(0); y < 64; y++ {
				c.SetBlock(x, y, z, 0, stoneRID)
			}
		}
	}

	blockEntities := benchmarkBlockEntities(b, count)
	for pos := range blockEntities {
		c.SetBlock(uint8(pos.X()), int16(pos.Y()), uint8(pos.Z()), 0, beaconRID)
	}
	col := &world.Column{
		Chunk:         c,
		BlockEntities: blockEntities,
	}
	return col, c.SubIndex(64)
}

func benchmarkSession(cacheEnabled bool) *Session {
	return &Session{
		conn:            benchmarkConn{cacheEnabled: cacheEnabled},
		conf:            Config{Log: slog.New(slog.NewTextHandler(io.Discard, nil))},
		blobs:           map[uint64][]byte{},
		closeBackground: make(chan struct{}),
	}
}

func benchmarkName(count int) string {
	return "count_" + strconv.Itoa(count)
}

type benchmarkConn struct {
	cacheEnabled bool
}

func (benchmarkConn) Close() error                                               { return nil }
func (benchmarkConn) IdentityData() login.IdentityData                           { return login.IdentityData{} }
func (benchmarkConn) ClientData() login.ClientData                               { return login.ClientData{} }
func (c benchmarkConn) ClientCacheEnabled() bool                                 { return c.cacheEnabled }
func (benchmarkConn) ChunkRadius() int                                           { return 8 }
func (benchmarkConn) Latency() time.Duration                                     { return 0 }
func (benchmarkConn) Flush() error                                               { return nil }
func (benchmarkConn) RemoteAddr() net.Addr                                       { return benchmarkAddr("") }
func (benchmarkConn) ReadPacket() (packet.Packet, error)                         { return nil, io.EOF }
func (benchmarkConn) WritePacket(packet.Packet) error                            { return nil }
func (benchmarkConn) StartGameContext(context.Context, minecraft.GameData) error { return nil }

type benchmarkAddr string

func (a benchmarkAddr) Network() string { return string(a) }
func (a benchmarkAddr) String() string  { return string(a) }

var _ Conn = benchmarkConn{}
var _ net.Addr = benchmarkAddr("")
