package player

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

func TestQuitAndSessionStopMayCloseAgain(t *testing.T) {
	w := world.Config{Log: slog.New(slog.DiscardHandler), GeneratorWorkers: 1}.New()
	defer w.Close()
	conn := newPacketPanicConn()
	blocked := make(chan string, 2)
	reenter := func(name string, f func()) {
		done := make(chan struct{})
		go func() { f(); close(done) }()
		select {
		case <-done:
		case <-time.After(time.Second):
			blocked <- name
		}
	}
	stops, quits := 0, 0
	var sess *session.Session
	sess = session.Config{
		Log: slog.New(slog.DiscardHandler), MaxChunkRadius: 1,
		HandleStop: func(tx *world.Tx, c session.Controllable) {
			stops++
			reenter("HandleStop", func() { sess.Close(tx, c) })
		},
	}.New(conn)
	defer sess.CloseConnection()
	pos := mgl64.Vec3{0.5, 64, 0.5}
	cfg := Config{Session: sess, Position: pos, UUID: uuid.MustParse(conn.identity.Identity), Name: conn.identity.DisplayName}
	handle := world.EntitySpawnOpts{Position: pos, ID: cfg.UUID}.New(Type, cfg)
	sess.SetHandle(handle, cfg.Skin)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := w.Do(func(tx *world.Tx) {
		p := tx.AddEntity(handle).(*Player)
		p.Handle(closeReentryHandler{onQuit: func(p *Player) {
			quits++
			reenter("HandleQuit", func() { p.Disconnect("Already quitting"); _ = p.Close() })
		}})
		sess.Spawn(p, tx)
	}).Wait(ctx); err != nil {
		t.Fatal(err)
	}
	conn.readErrors <- io.EOF
	select {
	case <-conn.closed:
	case <-ctx.Done():
		t.Fatal("connection remained open")
	}
	// The final owner barrier observes both callbacks and all entity cleanup.
	if err := w.Do(func(*world.Tx) {}).Wait(ctx); err != nil {
		t.Fatal(err)
	}
	if err := handle.Do(func(*world.Tx, world.Entity) {}).Wait(ctx); !errors.Is(err, world.ErrEntityClosed) {
		t.Fatalf("player still available after teardown: %v", err)
	}
	if stops != 1 || quits != 1 {
		t.Errorf("teardown callbacks: stops=%d quits=%d", stops, quits)
	}
	select {
	case name := <-blocked:
		t.Fatalf("%s blocked while closing again", name)
	default:
	}
}

type closeReentryHandler struct {
	NopHandler
	onQuit func(*Player)
}

func (h closeReentryHandler) HandleQuit(p *Player) { h.onQuit(p) }
