package session_test

import (
	"context"
	"io"
	"log/slog"
	"math"
	"net"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/netconn"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/login"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/go-gl/mathgl/mgl32"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

func TestEntityNetworkPositionConsistentAcrossPackets(t *testing.T) {
	pos := mgl64.Vec3{29_999_999, 100, -29_999_999}
	tests := []struct {
		name   string
		offset float64
		spawn  func() *world.EntityHandle
	}{
		{name: "player", offset: 1.62001, spawn: func() *world.EntityHandle {
			return world.EntitySpawnOpts{Position: pos, ID: uuid.New()}.New(player.Type, player.Config{
				UUID: uuid.New(), Name: "player", Position: pos,
			})
		}},
		// Client 1.26.51 ItemActor's constructor sets height offset to
		// 0.25 * 0.5, independently of ItemRenderer's visual bobbing.
		{name: "item", offset: 0.125, spawn: func() *world.EntityHandle {
			return entity.NewItem(world.EntitySpawnOpts{Position: pos}, item.NewStack(item.Diamond{}, 1))
		}},
		{name: "falling_block", offset: 0.5, spawn: func() *world.EntityHandle {
			return entity.NewFallingBlock(world.EntitySpawnOpts{Position: pos}, block.Sand{})
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			conn := &packetConn{packets: make(chan any, 64)}
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

			want := mgl32.Vec3{float32(pos[0]), float32(pos[1] + test.offset), float32(pos[2])}
			spawn, move, teleport := entityPacketPositions(t, conn.packets, test.name)
			for name, got := range map[string]mgl32.Vec3{"spawn": spawn, "move": move, "teleport": teleport} {
				if got != want {
					t.Fatalf("%s position = %v, want %v", name, got, want)
				}
			}
		})
	}
}

func TestDroppedItemNetworkHeightAboveSurface(t *testing.T) {
	for _, test := range []struct {
		name    string
		floor   world.Block
		surface float64
	}{
		{name: "full block", floor: block.Stone{}, surface: 1},
		{name: "bottom slab", floor: block.Slab{Block: block.Stone{}}, surface: 0.5},
		{name: "top slab", floor: block.Slab{Block: block.Stone{}, Top: true}, surface: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			conn := &packetConn{packets: make(chan any, 64)}
			viewer := session.Config{Log: slog.New(slog.DiscardHandler), MaxChunkRadius: 1}.New(conn)
			t.Cleanup(viewer.CloseConnection)
			w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
			t.Cleanup(func() { _ = w.Close() })
			var pos mgl64.Vec3
			var onGround bool
			if err := w.Do(func(tx *world.Tx) {
				tx.SetBlock(cube.Pos{0, 0, 0}, test.floor, nil)
				h := entity.NewItem(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 3, 0.5}}, item.NewStack(item.Diamond{}, 1))
				e := tx.AddEntity(h).(*entity.Ent)
				for tick := range 60 {
					e.Tick(tx, int64(tick))
				}
				pos, onGround = e.Position(), e.OnGround()
				viewer.ViewEntity(e)
				viewer.ViewEntityMovement(e, pos, cube.Rotation{}, onGround)
				viewer.ViewEntityTeleport(e, pos)
			}).Wait(context.Background()); err != nil {
				t.Fatalf("drop item: %v", err)
			}
			if !onGround || math.Abs(pos[1]-test.surface) > 1e-6 {
				t.Fatalf("item feet = %v, on ground = %v, want surface Y=%v", pos, onGround, test.surface)
			}
			// Native packets carry the actor origin, 1/8 block above its feet.
			// The client adds its own bobbing; no render offset belongs here.
			want := mgl32.Vec3{0.5, float32(test.surface + 0.125), 0.5}
			spawn, move, teleport := entityPacketPositions(t, conn.packets, "item")
			for name, got := range map[string]mgl32.Vec3{"spawn": spawn, "move": move, "teleport": teleport} {
				if got != want {
					t.Errorf("%s position = %v, want %v", name, got, want)
				}
			}
		})
	}
}

func TestDroppedItemMergeKeepsNetworkActor(t *testing.T) {
	conn := &packetConn{packets: make(chan any, 1024)}
	viewer := session.Config{Log: slog.New(slog.DiscardHandler), MaxChunkRadius: 1}.New(conn)
	t.Cleanup(viewer.CloseConnection)
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	var loader *world.Loader
	if err := w.Do(func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{8, 0, 8}, block.Stone{}, nil)
		loader = world.NewLoader(1, w, viewer)
		loader.Move(tx, mgl64.Vec3{8.5, 1, 8.5})
		loader.Load(tx, 1)
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Chunk publication is deferred until the loading transaction commits.
	if err := w.Do(func(tx *world.Tx) {
		defer loader.Close(tx)
		first := tx.AddEntity(entity.NewItem(world.EntitySpawnOpts{Position: mgl64.Vec3{8.4, 1, 8.5}}, item.NewStack(item.Diamond{}, 1))).(*entity.Ent)
		second := tx.AddEntity(entity.NewItem(world.EntitySpawnOpts{Position: mgl64.Vec3{8.6, 1, 8.5}}, item.NewStack(item.Diamond{}, 3))).(*entity.Ent)
		for tick := range 20 {
			for _, e := range []*entity.Ent{first, second} {
				if !e.H().Closed() {
					e.Tick(tx, int64(tick))
				}
			}
		}
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	const marker = "item-merge-done"
	viewer.SendMessage(marker)
	var spawns, updates int
	var survivorID, updatedID uint64
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case raw := <-conn.packets:
			switch pk := raw.(type) {
			case *v2193.AddItemActorPacket:
				spawns++
				if pk.Item.Stack.Count == 3 {
					survivorID = pk.EntityRuntimeID
				}
			case *v2193.ActorEventPacket:
				if pk.EventType == v2193.ActorEventUpdateStackSize {
					updates++
					updatedID = pk.EntityRuntimeID
					if pk.EventData != 4 {
						t.Errorf("stack size event=%d, want 4", pk.EventData)
					}
				}
			case *v2193.TextPacket:
				if pk.Message == marker {
					if spawns != 2 || updates != 1 || survivorID == 0 || updatedID != survivorID {
						t.Fatalf("spawns=%d updates=%d updated actor=%d, want two spawns and one update of actor %d", spawns, updates, updatedID, survivorID)
					}
					return
				}
			}
		case <-timer.C:
			t.Fatal("timed out waiting for item merge packets")
		}
	}
}

func entityPacketPositions(t *testing.T, packets <-chan any, entityType string) (spawn, move, teleport mgl32.Vec3) {
	t.Helper()
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for {
		select {
		case pk := <-packets:
			switch pk := pk.(type) {
			case *v2193.AddPlayerPacket:
				if entityType == "player" {
					spawn = pk.Position
				}
			case *v2193.AddItemActorPacket:
				if entityType == "item" {
					spawn = pk.Position
				}
			case *v2193.AddActorPacket:
				if entityType == "falling_block" {
					spawn = pk.Position
				}
			case *v2193.MovePlayerPacket:
				teleport = pk.Position
			case *v2193.MoveActorAbsolutePacket:
				if pk.Flags&v2193.MoveFlagTeleport != 0 {
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

// packetConn passes on the packets written to it, gophertunnel's and those
// of the generated codecs.
type packetConn struct {
	packets chan any
}

func (*packetConn) Close() error                                             { return nil }
func (*packetConn) ClientData() login.ClientData                             { return login.ClientData{} }
func (*packetConn) ClientCacheEnabled() bool                                 { return false }
func (*packetConn) ChunkRadius() int                                         { return 1 }
func (*packetConn) Latency() time.Duration                                   { return 0 }
func (*packetConn) Flush() error                                             { return nil }
func (*packetConn) RemoteAddr() net.Addr                                     { return &net.TCPAddr{} }
func (*packetConn) ReadPacket() (proto.Packet, error)                        { return nil, io.EOF }
func (c *packetConn) WritePacket(pk proto.Packet) error                      { c.packets <- pk; return nil }
func (c *packetConn) WriteProto(pk proto.Packet) error                       { c.packets <- pk; return nil }
func (*packetConn) StartGameContext(context.Context, netconn.GameData) error { return nil }
func (*packetConn) IdentityData() login.Identity {
	return login.Identity{Identity: uuid.NewString(), DisplayName: "viewer"}
}
