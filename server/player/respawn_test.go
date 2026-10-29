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
	"github.com/df-mc/dragonfly/server/netconn"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/login"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	v2193 "github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/go-gl/mathgl/mgl32"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

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

func TestPlayerReturnsFromEndToChargedNetherAnchor(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	spawn := cube.Pos{12, 64, -5}
	netherProvider := &playerSpawnPositionProvider{spawn: spawn}
	var end, nether *world.World
	end = world.Config{
		Log: log,
		Dim: world.End,
		PortalDestination: func(dim world.Dimension) *world.World {
			if dim == world.Nether {
				return nether
			}
			return nil
		},
		DefaultWorld: func() *world.World { return nether },
	}.New()
	nether = world.Config{
		Log:      log,
		Dim:      world.Nether,
		Provider: netherProvider,
		PortalDestination: func(dim world.Dimension) *world.World {
			if dim == world.End {
				return end
			}
			return nil
		},
	}.New()
	t.Cleanup(func() {
		_ = end.Close()
		_ = nether.Close()
	})

	conn := stubConn{}
	sess := session.Config{Log: log, MaxChunkRadius: 1}.New(conn)
	t.Cleanup(sess.CloseConnection)
	id := uuid.New()
	origin := mgl64.Vec3{5.5, 64, 5.5}
	velocity := mgl64.Vec3{0.25, 0.5, -0.75}
	cfg := Config{
		Session: sess, Position: origin, UUID: id, GameMode: world.GameModeSurvival,
		RespawnDimension: world.Nether, RespawnPosition: spawn, HasRespawnPoint: true,
	}
	handle := world.EntitySpawnOpts{Position: origin, Velocity: velocity, ID: id}.New(Type, cfg)
	sess.SetHandle(handle, cfg.Skin)
	nether.SetPlayerSpawn(id, spawn)

	if err := nether.Do(func(tx *world.Tx) {
		tx.SetBlock(spawn, block.RespawnAnchor{Charge: 1}, nil)
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := end.Do(func(tx *world.Tx) {
		tx.SetBlock(cube.PosFromVec3(origin), block.EndPortal{}, nil)
		player := tx.AddEntity(handle)
		(block.EndPortal{}).EntityInside(cube.PosFromVec3(origin), tx, player)
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}

	waitForPlayerWorld(t, handle, nether)
	if err := nether.Do(func(tx *world.Tx) {
		player, ok := handle.Entity(tx)
		if !ok {
			t.Error("player did not arrive in the Nether")
			return
		}
		if got, want := player.Position(), spawn.Vec3Middle(); !got.ApproxEqual(want) {
			t.Errorf("End return position = %v, want charged anchor position %v", got, want)
		}
		if got := player.(interface{ Velocity() mgl64.Vec3 }).Velocity(); got != (mgl64.Vec3{}) {
			t.Errorf("End player respawn velocity = %v, want Source absolute zero delta", got)
		}
		if anchor, ok := tx.Block(spawn).(block.RespawnAnchor); !ok || anchor.Charge != 1 {
			t.Errorf("End return consumed the respawn anchor: %T %+v", tx.Block(spawn), anchor)
		}
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}
func TestPlayerEndExitUsesOverworldBedPositionAndRotation(t *testing.T) {
	head := cube.Pos{0, 64, 0}
	safe := cube.Pos{-1, 64, -1}
	runPlayerEndExit(t, world.Overworld, head, func(tx *world.Tx) {
		tx.SetBlock(head, block.Bed{Facing: cube.East, Head: true}, nil)
		tx.SetBlock(head.Side(cube.FaceWest), block.Bed{Facing: cube.East}, nil)
		tx.SetBlock(safe.Side(cube.FaceDown), block.Stone{}, nil)
	}, safe.Vec3Middle(), cube.Rotation{-45, 0})
}

func TestPlayerEndExitFallsBackForUnchargedNetherAnchor(t *testing.T) {
	anchor := cube.Pos{12, 64, -5}
	spawn := cube.Pos{}
	runPlayerEndExit(t, world.Nether, anchor, func(tx *world.Tx) {
		tx.SetBlock(anchor, block.RespawnAnchor{}, nil)
	}, spawn.Vec3Middle(), cube.Rotation{})
}

func runPlayerEndExit(t *testing.T, dimension world.Dimension, respawnPos cube.Pos, setup func(*world.Tx), wantPos mgl64.Vec3, wantRotation cube.Rotation) {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	var end, destination *world.World
	end = world.Config{
		Log: log, Dim: world.End,
		PortalDestination: func(dim world.Dimension) *world.World {
			if dim == dimension {
				return destination
			}
			return nil
		},
		DefaultWorld: func() *world.World { return destination },
	}.New()
	destination = world.Config{Log: log, Dim: dimension}.New()
	t.Cleanup(func() {
		_ = end.Close()
		_ = destination.Close()
	})
	if err := destination.Do(setup).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	origin := mgl64.Vec3{5.5, 64, 5.5}
	id := uuid.New()
	cfg := Config{
		Position: origin, UUID: id, GameMode: world.GameModeSurvival,
		RespawnDimension: dimension, RespawnPosition: respawnPos, HasRespawnPoint: true,
	}
	handle := world.EntitySpawnOpts{Position: origin, ID: id, Velocity: mgl64.Vec3{.25, .5, -.75}}.New(Type, cfg)
	if err := end.Do(func(tx *world.Tx) {
		player := tx.AddEntity(handle)
		(block.EndPortal{}).EntityInside(cube.PosFromVec3(origin), tx, player)
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitForPlayerWorld(t, handle, destination)
	if err := destination.Do(func(tx *world.Tx) {
		player, ok := handle.Entity(tx)
		if !ok {
			t.Error("player did not arrive in the respawn dimension")
			return
		}
		if got := player.Position(); !got.ApproxEqual(wantPos) {
			t.Errorf("End return position = %v, want %v", got, wantPos)
		}
		if got := player.Rotation(); got != wantRotation {
			t.Errorf("End return rotation = %v, want %v", got, wantRotation)
		}
		if velocity := player.(interface{ Velocity() mgl64.Vec3 }).Velocity(); velocity != (mgl64.Vec3{}) {
			t.Errorf("End player respawn velocity = %v, want Source absolute zero delta", velocity)
		}
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestCancelledEndPortalTransferKeepsPlayerInSourceWorld(t *testing.T) {
	var end, overworld *world.World
	end = world.Config{
		Dim: world.End,
		PortalDestination: func(dim world.Dimension) *world.World {
			if dim == world.Overworld {
				return overworld
			}
			return nil
		},
		DefaultWorld: func() *world.World { return overworld },
	}.New()
	overworld = world.New()
	t.Cleanup(func() {
		_ = end.Close()
		_ = overworld.Close()
	})
	origin := mgl64.Vec3{5.5, 64, 5.5}
	rotation, velocity := cube.Rotation{12, -4}, mgl64.Vec3{0.25, 0.5, -0.75}
	id := uuid.New()
	handle := world.EntitySpawnOpts{Position: origin, Rotation: rotation, Velocity: velocity, ID: id}.New(Type, Config{
		UUID: id, Position: origin, Rotation: rotation, Velocity: velocity, GameMode: world.GameModeSurvival,
	})
	entered, release := make(chan struct{}), make(chan struct{})
	sourceSpawns := make(chan struct{}, 2)
	end.Handle(cancelledPlayerSpawnObserver{handle: handle, spawned: sourceSpawns})
	if err := end.Do(func(tx *world.Tx) {
		e := tx.AddEntity(handle).(*Player)
		e.Handle(cancelPortalTeleportHandler{entered: entered, release: release})
		(block.EndPortal{}).EntityInside(cube.PosFromVec3(origin), tx, e)
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-sourceSpawns:
	case <-time.After(5 * time.Second):
		t.Fatal("player was not initially published in the source End")
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("End portal transfer never reached the cancellable teleport handler")
	}
	if handle.World() != overworld {
		close(release)
		t.Fatalf("teleport handler ran while player was in world %p, want destination world", handle.World())
	}
	close(release)
	select {
	case <-sourceSpawns:
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled transfer did not republish player in the source End")
	}
	waitForPlayerWorld(t, handle, end)
	if err := end.Do(func(tx *world.Tx) {
		e, ok := handle.Entity(tx)
		if !ok || !e.Position().ApproxEqual(origin) {
			t.Fatalf("cancelled portal transfer entity=%v ok=%t; want source position %v", e, ok, origin)
		}
		if got := e.Rotation(); got != rotation {
			t.Fatalf("cancelled portal transfer rotation = %v, want %v", got, rotation)
		}
		if got := e.(interface{ Velocity() mgl64.Vec3 }).Velocity(); !got.ApproxEqual(velocity) {
			t.Fatalf("cancelled portal transfer velocity = %v, want %v", got, velocity)
		}
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}

type cancelledPlayerSpawnObserver struct {
	world.NopHandler
	handle  *world.EntityHandle
	spawned chan<- struct{}
}

func (o cancelledPlayerSpawnObserver) HandleEntitySpawn(_ *world.Tx, e world.Entity) {
	if e.H() == o.handle {
		o.spawned <- struct{}{}
	}
}

type cancelPortalTeleportHandler struct {
	NopHandler
	entered chan struct{}
	release <-chan struct{}
}

func (h cancelPortalTeleportHandler) HandleTeleport(ctx *Context, _ mgl64.Vec3) {
	close(h.entered)
	<-h.release
	ctx.Cancel()
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

		pk := &v2193.PlayerAuthInputPacket{Position: mgl32.Vec3{float32(origin[0]), float32(origin[1] + Type.NetworkOffset()), float32(origin[2])}}
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
