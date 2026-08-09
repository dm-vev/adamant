package session_test

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl32"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft"
	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func TestEntityNetworkPositionConsistentAcrossPackets(t *testing.T) {
	pos := mgl64.Vec3{29_999_999, 100, -29_999_999}
	tests := []struct {
		name  string
		spawn func() *world.EntityHandle
	}{
		{name: "player", spawn: func() *world.EntityHandle {
			return world.EntitySpawnOpts{Position: pos, ID: uuid.New()}.New(player.Type, player.Config{
				UUID: uuid.New(), Name: "player", Position: pos,
			})
		}},
		{name: "item", spawn: func() *world.EntityHandle {
			return entity.NewItem(world.EntitySpawnOpts{Position: pos}, item.NewStack(item.Diamond{}, 1))
		}},
		{name: "falling_block", spawn: func() *world.EntityHandle {
			return entity.NewFallingBlock(world.EntitySpawnOpts{Position: pos}, block.Sand{})
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			conn := &packetConn{packets: make(chan packet.Packet, 64)}
			viewer := session.Config{
				Log:            slog.New(slog.NewTextHandler(io.Discard, nil)),
				MaxChunkRadius: 1,
			}.New(conn)
			t.Cleanup(viewer.CloseConnection)

			w := world.New()
			t.Cleanup(func() { _ = w.Close() })
			handle := test.spawn()
			if err := w.Do(func(tx *world.Tx) {
				e := tx.AddEntity(handle)
				viewer.ViewEntity(e)
				viewer.ViewEntityMovement(e, pos, cube.Rotation{}, false)
				viewer.ViewEntityTeleport(e, pos)
			}).Wait(context.Background()); err != nil {
				t.Fatalf("send entity packets: %v", err)
			}

			offset := handle.Type().(interface{ NetworkOffset() float64 }).NetworkOffset()
			want := mgl32.Vec3{float32(pos[0]), float32(pos[1] + offset), float32(pos[2])}
			spawn, move, teleport := entityPacketPositions(t, conn.packets, test.name)
			for name, got := range map[string]mgl32.Vec3{"spawn": spawn, "move": move, "teleport": teleport} {
				if got != want {
					t.Fatalf("%s position = %v, want %v", name, got, want)
				}
			}
		})
	}
}

func entityPacketPositions(t *testing.T, packets <-chan packet.Packet, entityType string) (spawn, move, teleport mgl32.Vec3) {
	t.Helper()
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for {
		select {
		case pk := <-packets:
			switch pk := pk.(type) {
			case *packet.AddPlayer:
				if entityType == "player" {
					spawn = pk.Position
				}
			case *packet.AddItemActor:
				if entityType == "item" {
					spawn = pk.Position
				}
			case *packet.AddActor:
				if entityType == "falling_block" {
					spawn = pk.Position
				}
			case *packet.MovePlayer:
				teleport = pk.Position
			case *packet.MoveActorAbsolute:
				if pk.Flags&packet.MoveFlagTeleport != 0 {
					teleport = pk.Position
				} else {
					move = pk.Position
				}
			}
			if spawn != (mgl32.Vec3{}) && move != (mgl32.Vec3{}) && teleport != (mgl32.Vec3{}) {
				return
			}
		case <-timer.C:
			t.Fatal("timed out waiting for entity position packets")
		}
	}
}

type packetConn struct {
	packets chan packet.Packet
}

func (*packetConn) Close() error                                               { return nil }
func (*packetConn) ClientData() login.ClientData                               { return login.ClientData{} }
func (*packetConn) ClientCacheEnabled() bool                                   { return false }
func (*packetConn) ChunkRadius() int                                           { return 1 }
func (*packetConn) Latency() time.Duration                                     { return 0 }
func (*packetConn) Flush() error                                               { return nil }
func (*packetConn) RemoteAddr() net.Addr                                       { return &net.TCPAddr{} }
func (*packetConn) ReadPacket() (packet.Packet, error)                         { return nil, io.EOF }
func (c *packetConn) WritePacket(pk packet.Packet) error                       { c.packets <- pk; return nil }
func (*packetConn) StartGameContext(context.Context, minecraft.GameData) error { return nil }
func (*packetConn) IdentityData() login.IdentityData {
	return login.IdentityData{Identity: uuid.NewString(), DisplayName: "viewer"}
}
