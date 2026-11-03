package clienttest_test

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/client/clienttest"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/skin"
	"github.com/df-mc/dragonfly/server/text"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// Start records the Game and returns a Recorder that keeps it, answering
// with the profile and address of the login; Reject records its reason.
func TestJoinStartsARecorderOfTheGame(t *testing.T) {
	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 19132}
	j := &clienttest.Join{L: client.Login{
		Identity: client.Identity{Name: "Steve", UUID: uuid.New()},
		Profile:  client.Profile{DeviceID: "device"},
		Addr:     addr,
	}}
	if got := j.Login(); got.Identity.Name != "Steve" || got.Addr != addr {
		t.Errorf("Login() = %+v, want L", got)
	}
	c, err := j.Start(context.Background(), client.Game{Name: "server"})
	if err != nil {
		t.Fatal(err)
	}
	if games := j.Games(); len(games) != 1 || games[0].Name != "server" {
		t.Errorf("Games() = %v, want the Game passed", games)
	}
	r := j.Recorder()
	if r == nil || client.Joining(r) != c {
		t.Fatalf("Recorder() = %v, want the client Start returned", r)
	}
	if r.Profile().DeviceID != "device" || r.Addr() != addr {
		t.Error("the Recorder does not answer with the login")
	}
	j.Reject(text.Literal("bye"))
	if got := j.Rejected(); len(got) != 1 || !text.Equal(got[0], text.Literal("bye")) {
		t.Errorf("Rejected() = %v, want [bye]", got)
	}
}

// Start returns Err, or the error of a context that is done, and no
// Recorder.
func TestJoinStartFails(t *testing.T) {
	fail := errors.New("went away")
	j := &clienttest.Join{Err: fail}
	if _, err := j.Start(context.Background(), client.Game{}); !errors.Is(err, fail) {
		t.Errorf("Start() error = %v, want %v", err, fail)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	j = &clienttest.Join{}
	if _, err := j.Start(ctx, client.Game{}); !errors.Is(err, context.Canceled) {
		t.Errorf("Start() of a cancelled context: error = %v, want %v", err, context.Canceled)
	}
	if j.Recorder() != nil {
		t.Error("a Start that failed left a Recorder")
	}
	if len(j.Games()) != 1 {
		t.Error("the Game of a Start that failed was not recorded")
	}
}

// The Close of a Recorder that Start returned calls the Left hook of its
// Game once, with the player still in the world, then removes the player.
func TestJoinRecorderCloseCallsLeft(t *testing.T) {
	w := world.Config{Log: slog.New(slog.DiscardHandler)}.New()
	t.Cleanup(func() { _ = w.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var left int
	var inWorld bool
	j := &clienttest.Join{}
	c, err := j.Start(ctx, client.Game{Left: func(tx *world.Tx, p client.Controllable) {
		left++
		_, inWorld = p.H().Entity(tx)
	}})
	if err != nil {
		t.Fatal(err)
	}
	pos := mgl64.Vec3{0.5, 64, 0.5}
	handle := world.EntitySpawnOpts{Position: pos, ID: uuid.New()}.New(player.Type, player.Config{Position: pos, Client: c})
	c.Bind(handle, skin.Skin{})
	if err := w.Do(func(tx *world.Tx) {
		p := tx.AddEntity(handle).(*player.Player)
		c.Spawn(p, tx)
		c.Close(tx, p)
		c.Close(tx, p)
		if _, ok := handle.Entity(tx); ok {
			t.Error("the player is in the world after Close")
		}
	}).Wait(ctx); err != nil {
		t.Fatal(err)
	}
	if left != 1 || !inWorld {
		t.Errorf("Left was called %d times, the player in the world: %t; want once, in the world", left, inWorld)
	}
	r := j.Recorder()
	if len(r.CallsTo("Bind")) != 1 || len(r.CallsTo("Spawn")) != 1 {
		t.Error("Bind and Spawn were not recorded")
	}
}

// Accept yields the Joins sent, in order, and fails once the Listener is
// closed, which also releases a Send that waits.
func TestListenerYieldsJoinsUntilClosed(t *testing.T) {
	l := clienttest.NewListener()
	a, b := &clienttest.Join{}, &clienttest.Join{}
	go func() {
		l.Send(a)
		l.Send(b)
	}()
	for _, want := range []*clienttest.Join{a, b} {
		got, err := l.Accept()
		if err != nil || got != want {
			t.Fatalf("Accept() = %v, %v; want %v", got, err, want)
		}
	}
	sent := make(chan struct{})
	go func() {
		l.Send(&clienttest.Join{})
		close(sent)
	}()
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Accept(); !errors.Is(err, clienttest.ErrListenerClosed) {
		t.Errorf("Accept() after Close: error = %v, want %v", err, clienttest.ErrListenerClosed)
	}
	select {
	case <-sent:
	case <-time.After(5 * time.Second):
		t.Fatal("Send did not return after Close")
	}
	if err := l.Close(); err != nil {
		t.Errorf("second Close() = %v", err)
	}
}
