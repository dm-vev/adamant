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

type packetPanicHandler struct{ NopHandler }

func (packetPanicHandler) HandleChat(*Context, *string) { panic("packet handler panic") }

type packetPanicConn struct {
	identity login.IdentityData
	packets  chan packet.Packet
	closed   chan struct{}
	once     sync.Once
	writesMu sync.Mutex
	writes   []packet.Packet
}

func newPacketPanicConn() *packetPanicConn {
	return &packetPanicConn{
		identity: login.IdentityData{Identity: uuid.NewString(), DisplayName: "panic-test", XUID: "panic-xuid"},
		packets:  make(chan packet.Packet, 1),
		closed:   make(chan struct{}),
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
	case <-c.closed:
		return nil, io.EOF
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
