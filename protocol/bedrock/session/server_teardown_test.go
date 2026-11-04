package session_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"reflect"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/protocol/bedrock/netconn"
	"github.com/df-mc/dragonfly/protocol/bedrock/session"
	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/identity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/login"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// The tests of this file join sessions to a server.Server through its
// public API and a ConnListener of the test, and check how the server and
// the session end the session of a player together.

func TestServerStartGameUsesDefaultSpectatorProtocolGameType(t *testing.T) {
	ts := newTeardownServer(t, player.NopProvider{}, nil)
	ts.srv.World().SetDefaultGameMode(world.GameModeSpectator)

	conn := newTeardownConn()
	ts.l.conns <- conn
	data := <-conn.started
	ts.awaitSpawn(t)
	if data.PlayerGameMode != mc.GameTypeSpectator {
		t.Fatalf("StartGame player game type = %d, want spectator %d", data.PlayerGameMode, mc.GameTypeSpectator)
	}
}

func TestServerLeftPanicCompletesSessionTeardown(t *testing.T) {
	inv := inventory.New(36, nil)
	offHand := inventory.New(1, nil)
	armour := inventory.NewArmour(nil)
	_ = inv.SetItem(0, item.NewStack(item.Apple{}, 1))
	_ = offHand.SetItem(0, item.NewStack(item.Apple{}, 1))
	_ = armour.Inventory().SetItem(0, item.NewStack(item.Apple{}, 1))

	pos := mgl64.Vec3{0.5, 64, 0.5}
	provider := &panicSaveProvider{saved: make(chan struct{}), load: player.Config{
		Position: pos, Inventory: inv, OffHand: offHand, Armour: armour,
	}}
	ts := newTeardownServer(t, provider, nil)
	conn := newTeardownConn()
	id := uuid.MustParse(conn.identity.Identity)

	ts.l.conns <- conn
	handle := ts.awaitSpawn(t)
	waitForAnySessionViewer(t, ts.srv.World(), pos, true)

	conn.readErrors <- io.EOF
	select {
	case <-provider.saved:
	case <-time.After(5 * time.Second):
		t.Fatal("player provider Save was not called")
	}
	select {
	case <-conn.closed:
	case <-time.After(time.Second):
		t.Fatal("connection was not closed after Left panicked")
	}
	waitForAnySessionViewer(t, ts.srv.World(), pos, false)

	if _, ok := ts.srv.Player(id); ok {
		t.Fatal("player remained registered after Left panicked")
	}
	for name, got := range map[string]int{
		"inventory": len(inv.Items()),
		"off-hand":  len(offHand.Items()),
		"armour":    len(armour.Inventory().Items()),
	} {
		if got != 0 {
			t.Errorf("%s retained %d items after Left panicked", name, got)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := handle.Do(func(*world.Tx, world.Entity) {}).Wait(ctx); !errors.Is(err, world.ErrEntityClosed) {
		t.Fatalf("closed player task error = %v, want ErrEntityClosed", err)
	}
	if err := ts.srv.World().Do(func(*world.Tx) {}).Wait(ctx); err != nil {
		t.Fatalf("world owner stopped after Left panicked: %v", err)
	}

	// Close waits for every player the server made to leave.
	closed := make(chan struct{})
	go func() {
		_ = ts.srv.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("server player waitgroup was not released after Left panicked")
	}
}

func TestServerContainerRemoveViewerPanicDoesNotBlockShutdown(t *testing.T) {
	provider := &recordingSaveProvider{saved: make(chan struct{}, 1)}
	removed := make(chan struct{})
	ts := newTeardownServer(t, provider, func(p *player.Player) {
		tx := p.Tx()
		containerHandle := world.EntitySpawnOpts{Position: p.Position()}.New(panicRemoveViewerType{}, panicRemoveViewerConfig{removed: removed})
		container := tx.AddEntity(containerHandle)
		p.OpenEntityContainer(container, inventory.New(1, nil), entity.ContainerChestBoat, tx)
	})
	conn := newTeardownConn()
	ts.l.conns <- conn
	ts.awaitSpawn(t)

	conn.readErrors <- io.EOF
	select {
	case <-removed:
	case <-time.After(5 * time.Second):
		t.Fatal("container RemoveViewer was not called")
	}

	closed := make(chan struct{})
	go func() {
		_ = ts.srv.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("server shutdown blocked after RemoveViewer panic")
	}
	select {
	case <-provider.saved:
	default:
		t.Fatal("player was not saved during teardown")
	}
	if got := provider.saves.Load(); got != 1 {
		t.Fatalf("player save count = %d, want 1", got)
	}
}

// TestServerFailedJoinUnlistsPlayer checks that a player whose join fails
// in Accept, as its world closed before it spawned, leaves the player list
// of the other sessions: they are told it was removed, and a session made
// after it is not told of it.
func TestServerFailedJoinUnlistsPlayer(t *testing.T) {
	closedWorld := world.Config{Log: slog.New(slog.DiscardHandler), Generator: world.NopGenerator{}}.New()
	_ = closedWorld.Close()
	// The server keys its players by name: each client logs in under a name
	// of its own, and the record of the joining one is kept by the UUID of
	// its name.
	joining := newListConn()
	joining.identity.DisplayName = "Joining"
	id := uuid.MustParse(joining.identity.Identity)
	ts := newTeardownServer(t, closedWorldProvider{id: identity.OfflineUUID(joining.identity.DisplayName), w: closedWorld}, nil)

	observer := newListConn()
	observer.identity.DisplayName = "Observer"
	ts.l.conns <- observer
	ts.awaitSpawn(t)

	// The player of joining is loaded in a world that closed: Accept cannot
	// spawn it there.
	ts.l.conns <- joining
	observer.awaitList(t, mc.PlayerListActionAdd, id)
	observer.awaitList(t, mc.PlayerListActionRemove, id)

	later := newListConn()
	later.identity.DisplayName = "Later"
	ts.l.conns <- later
	after := ts.awaitSpawn(t)
	// The session made last is shown itself last.
	later.awaitList(t, mc.PlayerListActionAdd, after.UUID())
	for _, e := range later.listed() {
		if e == id {
			t.Fatal("a session made after the failed join was shown its player")
		}
	}
}

// teardownServer is a server.Server that a ConnListener of the test hands
// connections, and whose Accept runs in the background.
type teardownServer struct {
	srv     *server.Server
	l       *teardownListener
	spawned chan *world.EntityHandle
}

// newTeardownServer returns a started server for tests of sessions and
// their teardown. Its world generates no terrain: World.Close waits for the
// chunks being generated, which under the race detector takes seconds a
// chunk and has nothing to do with what these tests check. Accept runs
// spawn, if not nil, with each player it yields, in the transaction that
// spawned it.
func newTeardownServer(t *testing.T, provider player.Provider, spawn func(p *player.Player)) *teardownServer {
	t.Helper()
	return startTeardownServer(t, server.Config{
		Log:            slog.New(slog.DiscardHandler),
		PlayerProvider: provider,
		DisableNether:  true,
		DisableEnd:     true,
		MaxChunkRadius: 1,
	}, spawn)
}

// startTeardownServer makes a server of conf, whose only listener is that
// of the test and whose worlds generate no terrain, and starts it.
func startTeardownServer(t *testing.T, conf server.Config, spawn func(p *player.Player)) *teardownServer {
	t.Helper()
	ts := &teardownServer{
		l:       &teardownListener{conns: make(chan session.Conn, 4), closed: make(chan struct{})},
		spawned: make(chan *world.EntityHandle, 4),
	}
	conf.Generator = func(world.Dimension, int64) world.Generator { return world.NopGenerator{} }
	conf.Listeners = []func(server.Config, server.Host) (server.Listener, error){func(c server.Config, _ server.Host) (server.Listener, error) {
		return session.Listen(ts.l, session.ListenConfig{Log: c.Log}), nil
	}}
	ts.srv = conf.New()
	ts.srv.Listen()
	t.Cleanup(func() { _ = ts.srv.Close() })
	go func() {
		for p := range ts.srv.Accept() {
			if spawn != nil {
				spawn(p)
			}
			ts.spawned <- p.H()
		}
	}()
	return ts
}

// awaitSpawn waits until Accept yields a player and returns its handle.
func (ts *teardownServer) awaitSpawn(t *testing.T) *world.EntityHandle {
	t.Helper()
	select {
	case h := <-ts.spawned:
		return h
	case <-time.After(5 * time.Second):
		t.Fatal("the server did not spawn the player")
		return nil
	}
}

// teardownListener is the ConnListener of a teardownServer.
type teardownListener struct {
	conns     chan session.Conn
	closeOnce sync.Once
	closed    chan struct{}
}

func (l *teardownListener) Accept() (session.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.closed:
		return nil, errors.New("listener closed")
	}
}

func (*teardownListener) Disconnect(c session.Conn, _ string) error { return c.Close() }

func (l *teardownListener) Close() error {
	l.closeOnce.Do(func() { close(l.closed) })
	return nil
}

// waitForAnySessionViewer waits until a session views the position passed in
// w, or no longer does.
func waitForAnySessionViewer(t *testing.T, w *world.World, pos mgl64.Vec3, want bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		got, err := world.Call(context.Background(), w, func(tx *world.Tx) (bool, error) {
			viewers := tx.Viewers(pos)
			defer tx.ReleaseViewers(viewers)
			for _, viewer := range viewers {
				if _, ok := viewer.(*session.Session); ok {
					return true, nil
				}
			}
			return false, nil
		})
		if err != nil {
			t.Fatalf("inspect viewers: %v", err)
		}
		if got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("session viewer present = %t, want %t", got, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// panicSaveProvider loads the player as load says, in the world of its
// dimension, and panics as it saves it.
type panicSaveProvider struct {
	player.NopProvider
	load  player.Config
	saved chan struct{}
}

func (p *panicSaveProvider) Load(_ uuid.UUID, dim func(world.Dimension) *world.World) (player.Config, *world.World, error) {
	return p.load, dim(world.Overworld), nil
}

func (p *panicSaveProvider) Save(uuid.UUID, player.Config, *world.World) error {
	close(p.saved)
	panic("player provider save panic")
}

type recordingSaveProvider struct {
	player.NopProvider
	saved chan struct{}
	saves atomic.Int32
}

func (p *recordingSaveProvider) Save(uuid.UUID, player.Config, *world.World) error {
	p.saves.Add(1)
	p.saved <- struct{}{}
	return nil
}

// closedWorldProvider loads the player of id in w, and knows no other.
type closedWorldProvider struct {
	player.NopProvider
	id uuid.UUID
	w  *world.World
}

func (p closedWorldProvider) Load(id uuid.UUID, _ func(world.Dimension) *world.World) (player.Config, *world.World, error) {
	if id == p.id {
		return player.Config{}, p.w, nil
	}
	return player.Config{}, nil, errors.New("no data saved")
}

type panicRemoveViewerConfig struct {
	removed chan struct{}
}

func (c panicRemoveViewerConfig) Apply(data *world.EntityData) { data.Data = c.removed }

type panicRemoveViewerType struct{}

func (panicRemoveViewerType) Open(_ *world.Tx, h *world.EntityHandle, data *world.EntityData) world.Entity {
	return panicRemoveViewerEntity{handle: h, data: data}
}
func (panicRemoveViewerType) EncodeEntity() string                        { return "dragonfly:panic_container" }
func (panicRemoveViewerType) BBox(world.Entity) cube.BBox                 { return cube.BBox{} }
func (panicRemoveViewerType) DecodeNBT(map[string]any, *world.EntityData) {}
func (panicRemoveViewerType) EncodeNBT(*world.EntityData) map[string]any  { return nil }

type panicRemoveViewerEntity struct {
	handle *world.EntityHandle
	data   *world.EntityData
}

func (panicRemoveViewerEntity) Close() error              { return nil }
func (e panicRemoveViewerEntity) H() *world.EntityHandle  { return e.handle }
func (e panicRemoveViewerEntity) Position() mgl64.Vec3    { return e.data.Pos }
func (e panicRemoveViewerEntity) Rotation() cube.Rotation { return e.data.Rot }
func (e panicRemoveViewerEntity) RemoveViewer(block.ContainerViewer) {
	close(e.data.Data.(chan struct{}))
	panic("container RemoveViewer panic")
}

// teardownConn is a connection of a teardownServer: it logs in as a player
// of a UUID of its own, hands StartGame to started, and reads what is sent
// to readErrors as the error of a read.
type teardownConn struct {
	identity   login.Identity
	started    chan netconn.GameData
	readErrors chan error
	closed     chan struct{}
	once       sync.Once
}

func newTeardownConn() *teardownConn {
	return &teardownConn{
		identity:   login.Identity{Identity: uuid.NewString(), DisplayName: "panic-test", XUID: "panic-xuid"},
		started:    make(chan netconn.GameData, 1),
		readErrors: make(chan error, 1),
		closed:     make(chan struct{}),
	}
}

func (c *teardownConn) Close() error {
	c.once.Do(func() { close(c.closed) })
	return nil
}

func (c *teardownConn) IdentityData() login.Identity { return c.identity }
func (*teardownConn) ClientData() login.ClientData   { return login.ClientData{} }
func (*teardownConn) ClientCacheEnabled() bool       { return false }
func (*teardownConn) ChunkRadius() int               { return 1 }
func (*teardownConn) Latency() time.Duration         { return 0 }
func (*teardownConn) Flush() error                   { return nil }
func (*teardownConn) RemoteAddr() net.Addr           { return &net.TCPAddr{} }
func (*teardownConn) WritePacket(proto.Packet) error { return nil }

func (c *teardownConn) ReadPacket() (proto.Packet, error) {
	select {
	case err := <-c.readErrors:
		return nil, err
	case <-c.closed:
		return nil, io.EOF
	}
}

func (c *teardownConn) StartGameContext(_ context.Context, data netconn.GameData) error {
	select {
	case c.started <- data:
	default:
	}
	return nil
}

// listConn is a teardownConn that keeps the player list entries it is
// written.
type listConn struct {
	*teardownConn
	mu      sync.Mutex
	entries []mc.PlayerListEntry
	written chan struct{}
}

func newListConn() *listConn {
	return &listConn{teardownConn: newTeardownConn(), written: make(chan struct{}, 64)}
}

func (c *listConn) WritePacket(pk proto.Packet) error {
	// The session writes a player list entry it shares with every session
	// in a wrapper of its own, which embeds the packet.
	if v := reflect.ValueOf(pk); v.Kind() == reflect.Pointer && v.Elem().Kind() == reflect.Struct {
		if inner := v.Elem().FieldByName("Packet"); inner.IsValid() {
			pk, _ = inner.Interface().(proto.Packet)
		}
	}
	if list, ok := pk.(*mc.PlayerListPacket); ok {
		c.mu.Lock()
		c.entries = append(c.entries, list.Entries...)
		c.mu.Unlock()
		select {
		case c.written <- struct{}{}:
		default:
		}
	}
	return nil
}

// listed returns the UUIDs of the players the connection was told were
// added, in order, without those it was told were removed after.
func (c *listConn) listed() []uuid.UUID {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []uuid.UUID
	for _, e := range c.entries {
		if e.ActionType == mc.PlayerListActionRemove {
			out = slices.DeleteFunc(out, func(id uuid.UUID) bool { return id == uuid.UUID(e.UUID) })
			continue
		}
		out = append(out, uuid.UUID(e.UUID))
	}
	return out
}

// awaitList waits until the connection is written an entry of the action
// for the player of the UUID.
func (c *listConn) awaitList(t *testing.T, action byte, id uuid.UUID) {
	t.Helper()
	timeout := time.After(2 * time.Second)
	for {
		c.mu.Lock()
		found := slices.ContainsFunc(c.entries, func(e mc.PlayerListEntry) bool {
			return e.ActionType == action && uuid.UUID(e.UUID) == id
		})
		c.mu.Unlock()
		if found {
			return
		}
		select {
		case <-c.written:
		case <-timeout:
			t.Fatalf("the connection was not written the player list action %d of %s", action, id)
		}
	}
}
