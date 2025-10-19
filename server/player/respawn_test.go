package player

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"log/slog"

	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft"
	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

type stubConn struct{}

func (stubConn) Close() error { return nil }

func (stubConn) IdentityData() login.IdentityData {
	return login.IdentityData{Identity: uuid.NewString(), DisplayName: "Test"}
}

func (stubConn) ClientData() login.ClientData { return login.ClientData{} }

func (stubConn) ClientCacheEnabled() bool { return false }

func (stubConn) ChunkRadius() int { return 1 }

func (stubConn) Latency() time.Duration { return 0 }

func (stubConn) Flush() error { return nil }

func (stubConn) RemoteAddr() net.Addr { return &net.TCPAddr{} }

func (stubConn) ReadPacket() (packet.Packet, error) { return nil, io.EOF }

func (stubConn) WritePacket(packet.Packet) error { return nil }

func (stubConn) StartGameContext(context.Context, minecraft.GameData) error { return nil }

func TestRespawnFallsBackToDefaultDimension(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	dimensions := map[world.Dimension]*world.World{}

	var overworld *world.World
	overworldConf := world.Config{
		Log: log,
		PortalDestination: func(dim world.Dimension) *world.World {
			return dimensions[dim]
		},
	}
	overworldConf.DefaultWorld = func() *world.World { return overworld }
	overworld = overworldConf.New()
	dimensions[world.Overworld] = overworld
	t.Cleanup(func() {
		_ = overworld.Close()
	})

	netherConf := world.Config{
		Log: log,
		Dim: world.Nether,
		PortalDestination: func(dim world.Dimension) *world.World {
			return dimensions[dim]
		},
		DefaultWorld: func() *world.World { return overworld },
	}
	nether := netherConf.New()
	dimensions[world.Nether] = nether
	t.Cleanup(func() {
		_ = nether.Close()
	})

	conn := stubConn{}
	sess := session.Config{Log: log, MaxChunkRadius: 1}.New(conn)
	t.Cleanup(func() {
		sess.CloseConnection()
	})

	spawn := nether.Spawn().Vec3Centre().Add(mgl64.Vec3{0, 1.5})
	cfg := Config{
		Session:  sess,
		Position: spawn,
		GameMode: world.GameModeSurvival,
	}
	handle := world.EntitySpawnOpts{Position: spawn, ID: uuid.New()}.New(Type, cfg)
	sess.SetHandle(handle, cfg.Skin)

	<-nether.Exec(func(tx *world.Tx) {
		p := tx.AddEntity(handle).(*Player)
		p.addHealth(-p.MaxHealth())
		p.respawn(nil)
	})

	<-overworld.Exec(func(tx *world.Tx) {
		if _, ok := handle.Entity(tx); !ok {
			t.Fatalf("expected player to respawn in default world")
		}
	})

	<-nether.Exec(func(tx *world.Tx) {
		if _, ok := handle.Entity(tx); ok {
			t.Fatalf("expected player to leave origin dimension after respawn")
		}
	})
}
