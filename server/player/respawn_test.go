package player

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"log/slog"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl32"
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

func TestStationarySurvivalPlayerCompletesPortalTimer(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	var overworld, nether *world.World
	overworld = world.Config{Log: log, PortalDestination: func(dim world.Dimension) *world.World {
		if dim == world.Nether {
			return nether
		}
		return nil
	}}.New()
	nether = world.Config{Log: log, Dim: world.Nether, PortalDestination: func(dim world.Dimension) *world.World {
		if dim == world.Nether {
			return overworld
		}
		return nil
	}}.New()
	t.Cleanup(func() {
		_ = overworld.Close()
		_ = nether.Close()
	})

	conn := stubConn{}
	sess := session.Config{Log: log, MaxChunkRadius: 1}.New(conn)
	t.Cleanup(sess.CloseConnection)

	sourcePortal, targetPortal := cube.Pos{80, 64, 80}, cube.Pos{10, 64, 10}
	if err := nether.Do(func(tx *world.Tx) { buildPortalFrame(tx, targetPortal) }).Wait(context.Background()); err != nil {
		t.Fatalf("build destination portal: %v", err)
	}
	spawn := sourcePortal.Vec3Middle()
	cfg := Config{Session: sess, Position: spawn, GameMode: world.GameModeSurvival}
	handle := world.EntitySpawnOpts{Position: spawn, ID: uuid.New()}.New(Type, cfg)
	sess.SetHandle(handle, cfg.Skin)
	if err := overworld.Do(func(tx *world.Tx) {
		tx.SetBlock(sourcePortal, block.Portal{Axis: cube.Z}, nil)
		p := tx.AddEntity(handle).(*Player)
		p.Tick(tx, 1)
	}).Wait(context.Background()); err != nil {
		t.Fatalf("start stationary player portal timer: %v", err)
	}
	time.Sleep(4 * time.Second)
	if err := overworld.Do(func(tx *world.Tx) {
		p, ok := handle.Entity(tx)
		if !ok {
			t.Fatal("player left the source before the portal timer elapsed")
		}
		p.(*Player).Tick(tx, 2)
	}).Wait(context.Background()); err != nil {
		t.Fatalf("finish stationary player portal timer: %v", err)
	}

	waitForPlayerWorld(t, handle, nether)
}

func TestPlayerDisplacementRejectsStaleAuthInput(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	w := world.New()
	t.Cleanup(func() { _ = w.Close() })
	sess := session.Config{Log: log, MaxChunkRadius: 1}.New(stubConn{})
	t.Cleanup(sess.CloseConnection)

	origin := mgl64.Vec3{0.5, 64, 0.5}
	cfg := Config{Session: sess, Position: origin, GameMode: world.GameModeSurvival}
	handle := world.EntitySpawnOpts{Position: origin, ID: uuid.New()}.New(Type, cfg)
	sess.SetHandle(handle, cfg.Skin)
	if err := w.Do(func(tx *world.Tx) {
		p := tx.AddEntity(handle).(*Player)
		p.Displace(mgl64.Vec3{0.2})
		displaced := p.Position()

		pk := &packet.PlayerAuthInput{Position: mgl32.Vec3{float32(origin[0]), float32(origin[1] + 1.62), float32(origin[2])}}
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

func buildPortalFrame(tx *world.Tx, origin cube.Pos) {
	for z := range 2 {
		p := origin.Add(cube.Pos{0, 0, z})
		tx.SetBlock(p.Side(cube.FaceDown), block.Obsidian{}, nil)
		tx.SetBlock(p.Add(cube.Pos{0, 3}), block.Obsidian{}, nil)
	}
	for y := range 3 {
		p := origin.Add(cube.Pos{0, y})
		tx.SetBlock(p.Side(cube.FaceNorth), block.Obsidian{}, nil)
		tx.SetBlock(p.Add(cube.Pos{0, 0, 2}), block.Obsidian{}, nil)
		for z := range 2 {
			tx.SetBlock(p.Add(cube.Pos{0, 0, z}), block.Portal{Axis: cube.Z}, nil)
		}
	}
}

func waitForPlayerWorld(t *testing.T, handle *world.EntityHandle, target *world.World) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		inWorld, err := world.CallEntity(ctx, handle, func(tx *world.Tx, _ world.Entity) (bool, error) {
			return tx.World() == target, nil
		})
		cancel()
		if err == nil && inWorld {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for player portal travel")
}
