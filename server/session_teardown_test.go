package server

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

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/netconn"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/login"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

func TestStartGameUsesDefaultSpectatorProtocolGameType(t *testing.T) {
	srv := newSessionTestServer(t, player.NopProvider{})
	srv.world.SetDefaultGameMode(world.GameModeSpectator)

	conn := newServerPanicConn()
	done := make(chan struct{})
	go func() {
		srv.finaliseConn(context.Background(), conn, nil)
		close(done)
	}()

	data := <-conn.started
	inc := <-srv.incoming
	<-done
	t.Cleanup(func() {
		inc.s.CloseConnection()
		_ = inc.p.handle.Close()
		srv.pwg.Done()
	})
	if data.PlayerGameMode != v2193.GameTypeSpectator {
		t.Fatalf("StartGame player game type = %d, want spectator %d", data.PlayerGameMode, v2193.GameTypeSpectator)
	}
}

func TestHandleStopPanicCompletesServerSessionTeardown(t *testing.T) {
	provider := &panicSaveProvider{saved: make(chan struct{})}
	srv := newSessionTestServer(t, provider)
	conn := newServerPanicConn()
	id := uuid.MustParse(conn.identity.Identity)

	inv := inventory.New(36, nil)
	offHand := inventory.New(1, nil)
	armour := inventory.NewArmour(nil)
	_ = inv.SetItem(0, item.NewStack(item.Apple{}, 1))
	_ = offHand.SetItem(0, item.NewStack(item.Apple{}, 1))
	_ = armour.Inventory().SetItem(0, item.NewStack(item.Apple{}, 1))

	pos := mgl64.Vec3{0.5, 64, 0.5}
	inc := srv.createPlayer(id, conn, player.Config{
		Position: pos, Inventory: inv, OffHand: offHand, Armour: armour,
	}, srv.world)
	t.Cleanup(inc.s.CloseConnection)
	srv.players.add(inc.p)
	if err := srv.world.Do(func(tx *world.Tx) {
		p := tx.AddEntity(inc.p.handle).(*player.Player)
		inc.s.Spawn(p, tx)
	}).Wait(context.Background()); err != nil {
		t.Fatalf("spawn player: %v", err)
	}
	waitForServerSessionViewer(t, srv.world, pos, inc.s, true)

	conn.readErrors <- io.EOF
	select {
	case <-provider.saved:
	case <-time.After(5 * time.Second):
		t.Fatal("player provider Save was not called")
	}
	select {
	case <-conn.closed:
	case <-time.After(time.Second):
		t.Fatal("connection was not closed after HandleStop panic")
	}
	waitForServerSessionViewer(t, srv.world, pos, inc.s, false)

	waited := make(chan struct{})
	go func() {
		srv.pwg.Wait()
		close(waited)
	}()
	select {
	case <-waited:
	case <-time.After(5 * time.Second):
		t.Fatal("server player waitgroup was not released after HandleStop panic")
	}
	if _, ok := srv.Player(id); ok {
		t.Fatal("player remained registered after HandleStop panic")
	}
	for name, got := range map[string]int{
		"inventory": len(inv.Items()),
		"off-hand":  len(offHand.Items()),
		"armour":    len(armour.Inventory().Items()),
	} {
		if got != 0 {
			t.Errorf("%s retained %d items after HandleStop panic", name, got)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := inc.p.handle.Do(func(*world.Tx, world.Entity) {}).Wait(ctx); !errors.Is(err, world.ErrEntityClosed) {
		t.Fatalf("closed player task error = %v, want ErrEntityClosed", err)
	}
	if err := srv.world.Do(func(*world.Tx) {}).Wait(ctx); err != nil {
		t.Fatalf("world owner stopped after HandleStop panic: %v", err)
	}
}

func TestContainerRemoveViewerPanicDoesNotBlockServerShutdown(t *testing.T) {
	provider := &recordingSaveProvider{saved: make(chan struct{}, 1)}
	srv := newSessionTestServer(t, provider)
	conn := newServerPanicConn()
	id := uuid.MustParse(conn.identity.Identity)
	pos := mgl64.Vec3{0.5, 64, 0.5}
	inc := srv.createPlayer(id, conn, player.Config{Position: pos}, srv.world)
	srv.players.add(inc.p)

	removed := make(chan struct{})
	containerHandle := world.EntitySpawnOpts{Position: pos}.New(panicRemoveViewerType{}, panicRemoveViewerConfig{removed: removed})
	if err := srv.world.Do(func(tx *world.Tx) {
		p := tx.AddEntity(inc.p.handle).(*player.Player)
		container := tx.AddEntity(containerHandle)
		inc.s.Spawn(p, tx)
		p.OpenEntityContainer(container, inventory.New(1, nil), entity.ContainerChestBoat, tx)
	}).Wait(context.Background()); err != nil {
		t.Fatalf("spawn player and open container: %v", err)
	}

	srv.Listen()
	conn.readErrors <- io.EOF
	select {
	case <-removed:
	case <-time.After(5 * time.Second):
		t.Fatal("container RemoveViewer was not called")
	}

	closed := make(chan struct{})
	go func() {
		_ = srv.Close()
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

// newSessionTestServer returns a server for tests of sessions and their
// teardown. Its world generates no terrain: World.Close waits for the
// chunks being generated, which under the race detector takes seconds a
// chunk and has nothing to do with what these tests check.
func newSessionTestServer(t *testing.T, provider player.Provider) *Server {
	t.Helper()
	srv := Config{
		Log:                     slog.New(slog.NewTextHandler(io.Discard, nil)),
		PlayerProvider:          provider,
		DisableResourceBuilding: true,
		DisableNether:           true,
		DisableEnd:              true,
		MaxChunkRadius:          1,
		Generator:               func(world.Dimension, int64) world.Generator { return world.NopGenerator{} },
	}.New()
	closeWorlds(t, srv)
	return srv
}

func waitForServerSessionViewer(t *testing.T, w *world.World, pos mgl64.Vec3, sess *session.Session, want bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		got, err := world.Call(context.Background(), w, func(tx *world.Tx) (bool, error) {
			viewers := tx.Viewers(pos)
			defer tx.ReleaseViewers(viewers)
			for _, viewer := range viewers {
				if viewer == sess {
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

type panicSaveProvider struct {
	player.NopProvider
	saved chan struct{}
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

func (p *panicSaveProvider) Save(uuid.UUID, player.Config, *world.World) error {
	close(p.saved)
	panic("player provider save panic")
}

type serverPanicConn struct {
	identity   login.Identity
	started    chan netconn.GameData
	readErrors chan error
	closed     chan struct{}
	once       sync.Once
}

func newServerPanicConn() *serverPanicConn {
	return &serverPanicConn{
		identity:   login.Identity{Identity: uuid.NewString(), DisplayName: "panic-test", XUID: "panic-xuid"},
		started:    make(chan netconn.GameData, 1),
		readErrors: make(chan error, 1),
		closed:     make(chan struct{}),
	}
}

func (c *serverPanicConn) Close() error {
	c.once.Do(func() { close(c.closed) })
	return nil
}

func (c *serverPanicConn) IdentityData() login.Identity { return c.identity }
func (*serverPanicConn) ClientData() login.ClientData   { return login.ClientData{} }
func (*serverPanicConn) ClientCacheEnabled() bool       { return false }
func (*serverPanicConn) ChunkRadius() int               { return 1 }
func (*serverPanicConn) Latency() time.Duration         { return 0 }
func (*serverPanicConn) Flush() error                   { return nil }
func (*serverPanicConn) RemoteAddr() net.Addr           { return &net.TCPAddr{} }
func (*serverPanicConn) WritePacket(proto.Packet) error { return nil }

func (c *serverPanicConn) ReadPacket() (proto.Packet, error) {
	select {
	case err := <-c.readErrors:
		return nil, err
	case <-c.closed:
		return nil, io.EOF
	}
}

func (c *serverPanicConn) StartGameContext(_ context.Context, data netconn.GameData) error {
	c.started <- data
	return nil
}

// TestFailedJoinUnlistsPlayer checks that a player whose join fails in
// Accept, as its world closed before it spawned, leaves the player list of
// the other sessions: they are told it was removed, and a session made
// after it is not told of it.
func TestFailedJoinUnlistsPlayer(t *testing.T) {
	srv := newSessionTestServer(t, player.NopProvider{})

	observer := newListConn()
	obs := srv.createPlayer(uuid.MustParse(observer.identity.Identity), observer, player.Config{}, srv.world)
	t.Cleanup(func() {
		obs.s.CloseConnection()
		obs.s.Unbind()
		_ = obs.p.handle.Close()
		srv.pwg.Done()
	})
	joining := newListConn()
	id := uuid.MustParse(joining.identity.Identity)
	inc := srv.createPlayer(id, joining, player.Config{}, srv.world)
	observer.awaitList(t, v2193.PlayerListActionAdd, id)

	// The world closes before Accept spawns the player.
	_ = srv.world.Close()
	go func() {
		srv.incoming <- inc
		close(srv.incoming)
	}()
	for range srv.Accept() {
		t.Fatal("Accept yielded a player of a closed world")
	}
	observer.awaitList(t, v2193.PlayerListActionRemove, id)

	later := newListConn()
	after := srv.createPlayer(uuid.MustParse(later.identity.Identity), later, player.Config{}, srv.world)
	t.Cleanup(func() {
		after.s.CloseConnection()
		after.s.Unbind()
		_ = after.p.handle.Close()
		srv.pwg.Done()
	})
	// The session made last is shown itself last.
	later.awaitList(t, v2193.PlayerListActionAdd, after.p.handle.UUID())
	for _, e := range later.listed() {
		if e == id {
			t.Fatal("a session made after the failed join was shown its player")
		}
	}
}

// listConn is a serverPanicConn that keeps the player list entries it is
// written.
type listConn struct {
	*serverPanicConn
	mu      sync.Mutex
	entries []v2193.PlayerListEntry
	written chan struct{}
}

func newListConn() *listConn {
	return &listConn{serverPanicConn: newServerPanicConn(), written: make(chan struct{}, 64)}
}

func (c *listConn) WritePacket(pk proto.Packet) error {
	// The session writes a player list entry it shares with every session
	// in a wrapper of its own, which embeds the packet.
	if v := reflect.ValueOf(pk); v.Kind() == reflect.Pointer && v.Elem().Kind() == reflect.Struct {
		if inner := v.Elem().FieldByName("Packet"); inner.IsValid() {
			pk, _ = inner.Interface().(proto.Packet)
		}
	}
	if list, ok := pk.(*v2193.PlayerListPacket); ok {
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
		if e.ActionType == v2193.PlayerListActionRemove {
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
		found := slices.ContainsFunc(c.entries, func(e v2193.PlayerListEntry) bool {
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
