package session_test

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/netconn"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/login"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	v2193 "github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/go-gl/mathgl/mgl32"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// stubConn is a connection that reads nothing and drops what it is sent.
type stubConn struct{}

func (stubConn) Close() error { return nil }

func (stubConn) IdentityData() login.Identity {
	return login.Identity{Identity: uuid.NewString(), DisplayName: "Test"}
}

func (stubConn) ClientData() login.ClientData { return login.ClientData{} }

func (stubConn) ClientCacheEnabled() bool { return false }

func (stubConn) ChunkRadius() int { return 1 }

func (stubConn) Latency() time.Duration { return 0 }

func (stubConn) Flush() error { return nil }

func (stubConn) RemoteAddr() net.Addr { return &net.TCPAddr{} }

func (stubConn) ReadPacket() (proto.Packet, error) { return nil, io.EOF }

func (stubConn) WritePacket(proto.Packet) error { return nil }

func (stubConn) StartGameContext(context.Context, netconn.GameData) error { return nil }

func TestPlayerDisplacementRejectsStaleAuthInput(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	w := world.New()
	t.Cleanup(func() { _ = w.Close() })
	sess := session.Config{Log: log, MaxChunkRadius: 1}.New(stubConn{})
	t.Cleanup(sess.CloseConnection)

	origin := mgl64.Vec3{0.5, 64, 0.5}
	cfg := player.Config{Client: sess, Position: origin, GameMode: world.GameModeSurvival}
	handle := world.EntitySpawnOpts{Position: origin, ID: uuid.New()}.New(player.Type, cfg)
	sess.Bind(handle, cfg.Skin)
	if err := w.Do(func(tx *world.Tx) {
		p := tx.AddEntity(handle).(*player.Player)
		p.Displace(mgl64.Vec3{0.2})
		displaced := p.Position()

		pk := &v2193.PlayerAuthInputPacket{Position: mgl32.Vec3{float32(origin[0]), float32(origin[1] + player.Type.NetworkOffset()), float32(origin[2])}}
		if err := (session.PlayerAuthInputHandler{}).Handle(pk, sess, tx, p); err != nil {
			t.Fatalf("handle stale auth input: %v", err)
		}
		if got := p.Position(); !got.ApproxEqual(displaced) {
			t.Fatalf("stale auth input moved displaced player to %v, want %v", got, displaced)
		}
	}).Wait(context.Background()); err != nil {
		t.Fatalf("displace player: %v", err)
	}
}
