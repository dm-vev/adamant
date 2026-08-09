package player

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
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft"
	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func TestPacketHandlerPanicClosesOnlySession(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	w := world.Config{Log: log}.New()
	t.Cleanup(func() { _ = w.Close() })

	conn := newPacketPanicConn()
	stopped := make(chan *world.World, 1)
	sess := session.Config{
		Log:            log,
		MaxChunkRadius: 1,
		HandleStop: func(tx *world.Tx, _ session.Controllable) {
			stopped <- tx.World()
		},
	}.New(conn)
	t.Cleanup(sess.CloseConnection)

	pos := mgl64.Vec3{0.5, 64, 0.5}
	cfg := Config{Session: sess, Position: pos, UUID: uuid.MustParse(conn.identity.Identity), Name: conn.identity.DisplayName}
	h := world.EntitySpawnOpts{Position: pos, ID: cfg.UUID}.New(Type, cfg)
	sess.SetHandle(h, cfg.Skin)
	if err := w.Do(func(tx *world.Tx) {
		p := tx.AddEntity(h).(*Player)
		p.Handle(packetPanicHandler{})
		sess.Spawn(p, tx)
	}).Wait(context.Background()); err != nil {
		t.Fatalf("spawn player: %v", err)
	}

	conn.packets <- &packet.Text{
		TextType:   packet.TextTypeChat,
		SourceName: conn.identity.DisplayName,
		Message:    "panic",
		XUID:       conn.identity.XUID,
	}

	select {
	case owner := <-stopped:
		if owner != w {
			t.Fatalf("HandleStop owner = %p, want %p", owner, w)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("session was not stopped after packet handler panic")
	}
	select {
	case <-conn.closed:
	case <-time.After(time.Second):
		t.Fatal("connection was not closed after packet handler panic")
	}
	if !conn.disconnected() {
		t.Fatal("client was not sent a disconnect packet")
	}

	ran := false
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.Do(func(*world.Tx, world.Entity) { ran = true }).Wait(ctx); !errors.Is(err, world.ErrEntityClosed) {
		t.Fatalf("closed player task error = %v, want ErrEntityClosed", err)
	}
	if ran {
		t.Fatal("task ran for player closed after packet handler panic")
	}
	if err := w.Do(func(*world.Tx) {}).Wait(ctx); err != nil {
		t.Fatalf("world owner stopped after packet handler panic: %v", err)
	}
}

func TestHandleQuitPanicCompletesSessionTeardown(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	w := world.Config{Log: log}.New()
	t.Cleanup(func() { _ = w.Close() })

	conn := newPacketPanicConn()
	stopped := make(chan struct{}, 1)
	sess := session.Config{
		Log:            log,
		MaxChunkRadius: 1,
		HandleStop: func(*world.Tx, session.Controllable) {
			stopped <- struct{}{}
		},
	}.New(conn)
	t.Cleanup(sess.CloseConnection)

	inv := inventory.New(36, nil)
	offHand := inventory.New(1, nil)
	armour := inventory.NewArmour(nil)
	_ = inv.SetItem(0, item.NewStack(item.Apple{}, 1))
	_ = offHand.SetItem(0, item.NewStack(item.Apple{}, 1))
	_ = armour.Inventory().SetItem(0, item.NewStack(item.Apple{}, 1))

	pos := mgl64.Vec3{0.5, 64, 0.5}
	cfg := Config{
		Session: sess, Position: pos, UUID: uuid.MustParse(conn.identity.Identity), Name: conn.identity.DisplayName,
		Inventory: inv, OffHand: offHand, Armour: armour,
	}
	h := world.EntitySpawnOpts{Position: pos, ID: cfg.UUID}.New(Type, cfg)
	sess.SetHandle(h, cfg.Skin)
	if err := w.Do(func(tx *world.Tx) {
		p := tx.AddEntity(h).(*Player)
		p.Handle(quitPanicHandler{})
		sess.Spawn(p, tx)
	}).Wait(context.Background()); err != nil {
		t.Fatalf("spawn player: %v", err)
	}
	waitForSessionViewer(t, w, pos, sess, true)

	conn.readErrors <- io.EOF
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("HandleStop was not called after HandleQuit panic")
	}
	select {
	case <-conn.closed:
	case <-time.After(time.Second):
		t.Fatal("connection was not closed after HandleQuit panic")
	}
	waitForSessionViewer(t, w, pos, sess, false)

	for name, got := range map[string]int{
		"inventory": len(inv.Items()),
		"off-hand":  len(offHand.Items()),
		"armour":    len(armour.Inventory().Items()),
	} {
		if got != 0 {
			t.Errorf("%s retained %d items after HandleQuit panic", name, got)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.Do(func(*world.Tx, world.Entity) {}).Wait(ctx); !errors.Is(err, world.ErrEntityClosed) {
		t.Fatalf("closed player task error = %v, want ErrEntityClosed", err)
	}
	if err := w.Do(func(*world.Tx) {}).Wait(ctx); err != nil {
		t.Fatalf("world owner stopped after HandleQuit panic: %v", err)
	}
}

type packetPanicHandler struct{ NopHandler }

func (packetPanicHandler) HandleChat(*Context, *string) { panic("packet handler panic") }

type quitPanicHandler struct{ NopHandler }

func (quitPanicHandler) HandleQuit(*Player) { panic("quit handler panic") }

type packetPanicConn struct {
	identity   login.IdentityData
	packets    chan packet.Packet
	readErrors chan error
	closed     chan struct{}
	once       sync.Once
	writesMu   sync.Mutex
	writes     []packet.Packet
}

func newPacketPanicConn() *packetPanicConn {
	return &packetPanicConn{
		identity:   login.IdentityData{Identity: uuid.NewString(), DisplayName: "panic-test", XUID: "panic-xuid"},
		packets:    make(chan packet.Packet, 1),
		readErrors: make(chan error, 1),
		closed:     make(chan struct{}),
	}
}

func (c *packetPanicConn) Close() error {
	c.once.Do(func() { close(c.closed) })
	return nil
}

func (c *packetPanicConn) IdentityData() login.IdentityData { return c.identity }
func (*packetPanicConn) ClientData() login.ClientData       { return login.ClientData{} }
func (*packetPanicConn) ClientCacheEnabled() bool           { return false }
func (*packetPanicConn) ChunkRadius() int                   { return 1 }
func (*packetPanicConn) Latency() time.Duration             { return 0 }
func (*packetPanicConn) Flush() error                       { return nil }
func (*packetPanicConn) RemoteAddr() net.Addr               { return &net.TCPAddr{} }

func (c *packetPanicConn) ReadPacket() (packet.Packet, error) {
	select {
	case pk := <-c.packets:
		return pk, nil
	case err := <-c.readErrors:
		return nil, err
	case <-c.closed:
		return nil, io.EOF
	}
}

func waitForSessionViewer(t *testing.T, w *world.World, pos mgl64.Vec3, sess *session.Session, want bool) {
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

func (c *packetPanicConn) WritePacket(pk packet.Packet) error {
	c.writesMu.Lock()
	c.writes = append(c.writes, pk)
	c.writesMu.Unlock()
	return nil
}

func (*packetPanicConn) StartGameContext(context.Context, minecraft.GameData) error { return nil }

func (c *packetPanicConn) disconnected() bool {
	c.writesMu.Lock()
	defer c.writesMu.Unlock()
	for _, pk := range c.writes {
		if _, ok := pk.(*packet.Disconnect); ok {
			return true
		}
	}
	return false
}
