package server

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/client/clienttest"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/chat"
	"github.com/df-mc/dragonfly/server/text"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
	"golang.org/x/text/language"
)

// The tests of this file join clients of clienttest to a Server through a
// clienttest.Listener, and check how the server answers their Joins.

// TestJoinStartsTheGameOfThePlayer checks the Game a client starts in and
// that the player of the client is bound and spawned.
func TestJoinStartsTheGameOfThePlayer(t *testing.T) {
	jt := newJoinTest(t, player.NopProvider{}, Config{Name: "Join Test", MaxChunkRadius: 3, JoinMessage: chat.MessageJoin, QuitMessage: chat.MessageQuit})
	var err error
	jt.srv.operators, err = loadOperators(filepath.Join(t.TempDir(), "ops.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := jt.srv.Op("Alice"); err != nil {
		t.Fatal(err)
	}
	jt.start()

	alice := joinLogin("Alice", "2535400000000001")
	alice.L.Locale = language.BritishEnglish
	handle := jt.join(t, alice)
	bob := joinLogin("Bob", "")
	jt.join(t, bob)

	games := alice.Games()
	if len(games) != 1 {
		t.Fatalf("Alice started %d games, want one", len(games))
	}
	g := games[0]
	if g.Name != "Join Test" || g.Level != jt.srv.World() || g.World != jt.srv.World() || g.MaxChunkRadius != 3 {
		t.Errorf("Game = %+v, want the server's name, world and chunk radius", g)
	}
	if !g.Operator || g.IsOperator == nil || !g.IsOperator("alice") || g.IsOperator("bob") {
		t.Error("the Game of Alice does not hold her an operator")
	}
	if g.JoinMessage != chat.MessageJoin || g.QuitMessage != chat.MessageQuit || g.Left == nil {
		t.Error("the Game does not carry the join and quit messages and the Left hook of the server")
	}
	if bob.Games()[0].Operator {
		t.Error("the Game of Bob holds him an operator")
	}

	var methods []string
	for _, c := range alice.Recorder().Calls() {
		if c.Method == "Bind" || c.Method == "Spawn" {
			methods = append(methods, c.Method)
		}
	}
	if !slices.Equal(methods, []string{"Bind", "Spawn"}) {
		t.Errorf("Alice's client was called %v, want Bind, then Spawn", methods)
	}
	if bind := alice.Recorder().CallsTo("Bind"); bind[0].Args[0] != handle {
		t.Error("Alice's client was bound to another handle than that of her player")
	}
	if h, ok := jt.srv.PlayerByXUID("2535400000000001"); !ok || h != handle {
		t.Error("Alice is not online by her XUID")
	}
	locale, _ := player.Call(context.Background(), handle, func(_ *world.Tx, p *player.Player) (language.Tag, error) {
		return p.Locale(), nil
	})
	if locale != language.BritishEnglish {
		t.Errorf("Alice plays in %v, want the locale she logged in with", locale)
	}
}

// TestJoinReconnectKickJoining checks that a client logging in as a player
// online is rejected by default, and its game not started.
func TestJoinReconnectKickJoining(t *testing.T) {
	jt := newJoinTest(t, player.NopProvider{}, Config{})
	jt.start()
	first := joinLogin("Alice", "")
	jt.join(t, first)

	again := &clienttest.Join{L: first.L}
	jt.l.Send(again)
	awaitRejected(t, again, "Already logged in.")
	if len(again.Games()) != 0 {
		t.Error("the game of a client rejected as a player online was started")
	}
	if len(first.Recorder().CallsTo("Disconnect")) != 0 {
		t.Error("the player online was disconnected")
	}
}

// TestJoinReconnectKickExisting checks that a client logging in as a player
// online has the player online disconnected and saved, and then joins.
func TestJoinReconnectKickExisting(t *testing.T) {
	provider := &joinSaveProvider{}
	jt := newJoinTest(t, provider, Config{ReconnectPolicy: ReconnectKickExisting})
	jt.start()
	first := joinLogin("Alice", "")
	old := jt.join(t, first)

	again := &clienttest.Join{L: first.L}
	if h := jt.join(t, again); h == old {
		t.Fatal("the client logging in again got the handle of the player online")
	}
	if len(first.Recorder().CallsTo("Disconnect")) != 1 || len(first.Recorder().CallsTo("Close")) == 0 {
		t.Error("the player online was not disconnected and closed")
	}
	if got := provider.saved(); !slices.Equal(got, []uuid.UUID{first.L.Identity.UUID}) {
		t.Errorf("saved %v, want the player online once", got)
	}
}

// TestJoinStartFailureRejects checks that a client whose game does not
// start is rejected with "Connection timeout." and no player of it is made.
func TestJoinStartFailureRejects(t *testing.T) {
	jt := newJoinTest(t, player.NopProvider{}, Config{})
	jt.start()
	j := joinLogin("Alice", "")
	j.Err = errors.New("the client went away")
	jt.l.Send(j)
	awaitRejected(t, j, "Connection timeout.")
	if len(j.Games()) != 1 {
		t.Errorf("Start was called %d times, want once", len(j.Games()))
	}
	if n := jt.srv.PlayerCount(); n != 0 {
		t.Errorf("%d players online, want none", n)
	}
	// The player joins from another client as if the first never had.
	jt.join(t, &clienttest.Join{L: j.L})
}

// TestJoinLeftSavesThePlayer checks that the Left hook of the Game, which
// the client calls as it closes, saves the player and stops counting it
// online.
func TestJoinLeftSavesThePlayer(t *testing.T) {
	provider := &joinSaveProvider{}
	jt := newJoinTest(t, provider, Config{})
	jt.start()
	j := joinLogin("Alice", "")
	handle := jt.join(t, j)

	if err := player.Do(handle, func(_ *world.Tx, p *player.Player) { p.Disconnect("bye") }).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := provider.saved(); !slices.Equal(got, []uuid.UUID{j.L.Identity.UUID}) {
		t.Errorf("saved %v, want Alice once", got)
	}
	if _, ok := jt.srv.Player(j.L.Identity.UUID); ok {
		t.Error("Alice is online after she left")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := handle.Do(func(*world.Tx, world.Entity) {}).Wait(ctx); !errors.Is(err, world.ErrEntityClosed) {
		t.Errorf("the handle of Alice after she left: err = %v, want %v", err, world.ErrEntityClosed)
	}
}

// joinTest is a Server that a clienttest.Listener hands Joins, and whose
// Accept runs in the background.
type joinTest struct {
	srv     *Server
	l       *clienttest.Listener
	spawned chan *world.EntityHandle
}

// newJoinTest makes a server of conf, with the provider passed, whose only
// listener is that of the test and whose worlds generate no terrain. start
// starts it.
func newJoinTest(t *testing.T, provider player.Provider, conf Config) *joinTest {
	t.Helper()
	jt := &joinTest{l: clienttest.NewListener(), spawned: make(chan *world.EntityHandle, 4)}
	conf.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	conf.PlayerProvider = provider
	conf.DisableResourceBuilding = true
	conf.DisableNether, conf.DisableEnd = true, true
	conf.Generator = func(world.Dimension, int64) world.Generator { return world.NopGenerator{} }
	conf.Listeners = []func(Config) (Listener, error){func(Config) (Listener, error) { return jt.l, nil }}
	jt.srv = conf.New()
	return jt
}

// start starts the server and accepts its players in the background.
func (jt *joinTest) start() {
	jt.srv.Listen()
	go func() {
		for p := range jt.srv.Accept() {
			jt.spawned <- p.H()
		}
	}()
}

// join sends j and waits until its player spawns.
func (jt *joinTest) join(t *testing.T, j *clienttest.Join) *world.EntityHandle {
	t.Helper()
	jt.l.Send(j)
	select {
	case h := <-jt.spawned:
		if h.UUID() != j.L.Identity.UUID {
			t.Fatalf("the server spawned %s, want %s", h.UUID(), j.L.Identity.UUID)
		}
		t.Cleanup(func() { _ = jt.srv.Close() })
		return h
	case <-time.After(5 * time.Second):
		t.Fatalf("the server did not spawn %s", j.L.Identity.Name)
		return nil
	}
}

// joinLogin returns a Join of a client that logs in as name, with the
// XUID passed and a UUID of its own.
func joinLogin(name, xuid string) *clienttest.Join {
	return &clienttest.Join{L: client.Login{
		Identity: client.Identity{Name: name, UUID: uuid.New(), XUID: xuid},
		Locale:   language.English,
		Addr:     &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 19132},
	}}
}

// awaitRejected waits until j is rejected, and checks that it was rejected
// once, with the reason passed.
func awaitRejected(t *testing.T, j *clienttest.Join, reason string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for len(j.Rejected()) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the client was not rejected")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := j.Rejected(); len(got) != 1 || !text.Equal(got[0], text.Literal(reason)) {
		t.Errorf("the client was rejected with %v, want %q once", got, reason)
	}
}

// joinSaveProvider records the UUIDs of the players it saves, and loads
// none.
type joinSaveProvider struct {
	player.NopProvider
	mu    sync.Mutex
	saves []uuid.UUID
}

func (p *joinSaveProvider) Save(id uuid.UUID, _ player.Config, _ *world.World) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.saves = append(p.saves, id)
	return nil
}

func (p *joinSaveProvider) saved() []uuid.UUID {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.saves)
}
