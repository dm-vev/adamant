package server

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft"
	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
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
	if data.PlayerGameMode != packet.GameTypeSpectator {
		t.Fatalf("StartGame player game type = %d, want spectator %d", data.PlayerGameMode, packet.GameTypeSpectator)
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
	srv.pmu.Lock()
	srv.p[id] = inc.p
	srv.pmu.Unlock()
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

func newSessionTestServer(t *testing.T, provider player.Provider) *Server {
	t.Helper()
	srv := Config{
		Log:                     slog.New(slog.NewTextHandler(io.Discard, nil)),
		PlayerProvider:          provider,
		DisableResourceBuilding: true,
		DisableNether:           true,
		DisableEnd:              true,
		MaxChunkRadius:          1,
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

func (p *panicSaveProvider) Save(uuid.UUID, player.Config, *world.World) error {
	close(p.saved)
	panic("player provider save panic")
}

type serverPanicConn struct {
	identity   login.IdentityData
	started    chan minecraft.GameData
	readErrors chan error
	closed     chan struct{}
	once       sync.Once
}

func newServerPanicConn() *serverPanicConn {
	return &serverPanicConn{
		identity:   login.IdentityData{Identity: uuid.NewString(), DisplayName: "panic-test", XUID: "panic-xuid"},
		started:    make(chan minecraft.GameData, 1),
		readErrors: make(chan error, 1),
		closed:     make(chan struct{}),
	}
}

func (c *serverPanicConn) Close() error {
	c.once.Do(func() { close(c.closed) })
	return nil
}

func (c *serverPanicConn) IdentityData() login.IdentityData { return c.identity }
func (*serverPanicConn) ClientData() login.ClientData       { return login.ClientData{} }
func (*serverPanicConn) ClientCacheEnabled() bool           { return false }
func (*serverPanicConn) ChunkRadius() int                   { return 1 }
func (*serverPanicConn) Latency() time.Duration             { return 0 }
func (*serverPanicConn) Flush() error                       { return nil }
func (*serverPanicConn) RemoteAddr() net.Addr               { return &net.TCPAddr{} }
func (*serverPanicConn) WritePacket(packet.Packet) error    { return nil }

func (c *serverPanicConn) ReadPacket() (packet.Packet, error) {
	select {
	case err := <-c.readErrors:
		return nil, err
	case <-c.closed:
		return nil, io.EOF
	}
}

func (c *serverPanicConn) StartGameContext(_ context.Context, data minecraft.GameData) error {
	c.started <- data
	return nil
}
