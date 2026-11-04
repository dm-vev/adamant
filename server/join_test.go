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
	"github.com/df-mc/dragonfly/server/identity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
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
	bobHandle := jt.join(t, bob)

	games := alice.Games()
	if len(games) != 1 {
		t.Fatalf("Alice started %d games, want one", len(games))
	}
	g := games[0]
	if g.Name != "Join Test" || g.Level != jt.srv.World() || g.World != jt.srv.World() || g.MaxChunkRadius != 3 {
		t.Errorf("Game = %+v, want the server's name, world and chunk radius", g)
	}
	if !g.Operator || g.IsOperator == nil || !g.IsOperator(handle.UUID()) || g.IsOperator(bobHandle.UUID()) {
		t.Error("the Game of Alice does not hold her an operator")
	}
	conf, _ := player.Call(context.Background(), handle, func(_ *world.Tx, p *player.Player) (player.Config, error) {
		return p.Data(), nil
	})
	if conf.QuitMessage != chat.MessageQuit || conf.Left == nil {
		t.Error("the player of Alice does not carry the quit message and the Left hook of the server")
	}
	// The server welcomed Bob after his client spawned him: Alice, already
	// subscribed to the chat, was told he joined.
	want := chat.MessageJoin.F("Bob").Text(language.BritishEnglish).Legacy()
	var told bool
	for _, c := range alice.Recorder().CallsTo("SendMessage") {
		if msg, ok := c.Args[0].(text.Text); ok && msg.Legacy() == want {
			told = true
		}
	}
	if !told {
		t.Errorf("Alice was not told %q", want)
	}
	if bob.Games()[0].Operator {
		t.Error("the Game of Bob holds him an operator")
	}
	if g.Roster != &jt.srv.players.roster || bob.Games()[0].Roster != g.Roster {
		t.Error("the Games do not share the Roster of the server")
	}
	// Bob, bound after Alice, was shown her, and she him.
	if !listedTo(alice.Recorder(), "Bob") || !listedTo(bob.Recorder(), "Alice") {
		t.Error("the clients of Alice and Bob were not shown each other")
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

	// Bob, leaving, has the quit message of the server written to the chat:
	// Alice, still subscribed, is told he left.
	if err := player.Do(bobHandle, func(_ *world.Tx, p *player.Player) { p.Disconnect("bye") }).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	want = chat.MessageQuit.F("Bob").Text(language.BritishEnglish).Legacy()
	told = false
	for _, c := range alice.Recorder().CallsTo("SendMessage") {
		if msg, ok := c.Args[0].(text.Text); ok && msg.Legacy() == want {
			told = true
		}
	}
	if !told {
		t.Errorf("Alice was not told %q", want)
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
	if got := provider.saved(); !slices.Equal(got, []uuid.UUID{identity.OfflineUUID("Alice")}) {
		t.Errorf("saved %v, want the player online once, by its name", got)
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

// TestJoinSpawnTimeoutRejects checks that a client that never spawns is
// rejected with SpawnTimeoutMessage once SpawnTimeout passed, holding no
// player, and that its player then joins from another client.
func TestJoinSpawnTimeoutRejects(t *testing.T) {
	jt := newJoinTest(t, player.NopProvider{}, Config{SpawnTimeout: 50 * time.Millisecond, SpawnTimeoutMessage: "Too slow."})
	jt.start()
	j := joinLogin("Alice", "")
	j.Stall = true
	began := time.Now()
	jt.l.Send(j)
	awaitRejected(t, j, "Too slow.")
	if waited := time.Since(began); waited < 50*time.Millisecond {
		t.Errorf("rejected after %v, before the timeout", waited)
	}
	if n := jt.srv.PlayerCount(); n != 0 {
		t.Errorf("%d players online, want none", n)
	}
	jt.join(t, &clienttest.Join{L: j.L})
}

// TestJoinSpawnTimeoutDefaults checks the SpawnTimeout and its message of a
// Config that sets neither.
func TestJoinSpawnTimeoutDefaults(t *testing.T) {
	jt := newJoinTest(t, player.NopProvider{}, Config{})
	jt.start()
	t.Cleanup(func() { _ = jt.srv.Close() })
	if jt.srv.conf.SpawnTimeout != DefaultSpawnTimeout || jt.srv.conf.SpawnTimeoutMessage != DefaultSpawnTimeoutMessage {
		t.Errorf("SpawnTimeout %v with %q, want %v with %q", jt.srv.conf.SpawnTimeout, jt.srv.conf.SpawnTimeoutMessage, DefaultSpawnTimeout, DefaultSpawnTimeoutMessage)
	}
}

// TestParseSpawnTimeout checks how Server.SpawnTimeout of config.toml reads.
func TestParseSpawnTimeout(t *testing.T) {
	for _, tc := range []struct {
		s    string
		want time.Duration
	}{{"", 0}, {" 1m ", time.Minute}, {"90s", 90 * time.Second}, {"0s", -1}, {"-5s", -1}} {
		s, want := tc.s, tc.want
		if got, err := parseSpawnTimeout(s); err != nil || got != want {
			t.Errorf("parseSpawnTimeout(%q) = %v, %v, want %v", s, got, err, want)
		}
	}
	if _, err := parseSpawnTimeout("soon"); err == nil {
		t.Error("parseSpawnTimeout took \"soon\"")
	}
}

// TestJoinLeftSavesThePlayer checks that the Left hook the server gives a
// player, which it calls as it leaves, saves the player, in inventories of
// the data saved and not its own, and stops counting it online, and that
// the client unlists it.
func TestJoinLeftSavesThePlayer(t *testing.T) {
	provider := &joinSaveProvider{}
	jt := newJoinTest(t, provider, Config{})
	jt.start()
	j := joinLogin("Alice", "")
	handle := jt.join(t, j)

	var live *inventory.Inventory
	if err := player.Do(handle, func(_ *world.Tx, p *player.Player) {
		live = p.Inventory()
		_ = live.SetItem(4, item.NewStack(item.Diamond{}, 10))
		p.Disconnect("bye")
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := provider.saved(); !slices.Equal(got, []uuid.UUID{identity.OfflineUUID("Alice")}) {
		t.Errorf("saved %v, want Alice once, by her name", got)
	}
	if data := provider.data(); len(data) == 1 {
		if data[0].Inventory == live {
			t.Error("saved the live inventory of Alice")
		}
		if it, _ := data[0].Inventory.Item(4); it.Count() != 10 {
			t.Errorf("saved %v in slot 4, want the 10 diamonds of Alice", it)
		}
	}
	if _, ok := jt.srv.Player(j.L.Identity.UUID); ok {
		t.Error("Alice is online after she left")
	}
	if len(jt.srv.players.roster.Players()) != 0 {
		t.Error("Alice is listed after she left")
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
	conf.DisableNether, conf.DisableEnd = true, true
	conf.Generator = func(world.Dimension, int64) world.Generator { return world.NopGenerator{} }
	conf.Listeners = []func(Config, Host) (Listener, error){func(Config, Host) (Listener, error) { return jt.l, nil }}
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

// listedTo reports whether r was shown the player of the name passed.
func listedTo(r *clienttest.Recorder, name string) bool {
	return slices.ContainsFunc(r.CallsTo("ListPlayer"), func(c clienttest.Call) bool {
		return c.Args[0].(client.Listed).Identity.Name == name
	})
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
	confs []player.Config
}

func (p *joinSaveProvider) Save(id uuid.UUID, conf player.Config, _ *world.World) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.saves = append(p.saves, id)
	p.confs = append(p.confs, conf)
	return nil
}

// data returns the data of the players saved, in the order they were.
func (p *joinSaveProvider) data() []player.Config {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.confs)
}

func (p *joinSaveProvider) saved() []uuid.UUID {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.saves)
}
