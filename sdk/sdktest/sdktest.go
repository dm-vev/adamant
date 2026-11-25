// Package sdktest provides a lightweight test harness for writing fast,
// deterministic, race-clean unit and integration tests for Adamant plugins.
package sdktest

import (
	"io"
	"log/slog"
	"testing"

	"github.com/df-mc/dragonfly/sdk/plugin"
	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/client/clienttest"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/skin"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// NewServer starts an isolated, silent in-memory *server.Server with no
// network listeners and registers automatic cleanup on t.
func NewServer(t testing.TB) *server.Server {
	t.Helper()
	srv := server.Config{
		Log:             slog.New(slog.NewTextHandler(io.Discard, nil)),
		WorldProvider:   world.NopProvider{},
		PlayerProvider:  player.NopProvider{},
		Generator:       func(world.Dimension, int64) world.Generator { return world.NopGenerator{} },
		RandomTickSpeed: -1,
		SaveInterval:    -1,
	}.New()
	srv.Listen()
	t.Cleanup(func() { _ = srv.Close() })
	return srv
}

// NewApp creates a *plugin.App wired to a fresh in-memory test server and a
// temporary plugin data directory, automatically disabling plugins on cleanup.
func NewApp(t testing.TB, opts ...plugin.AppOption) *plugin.App {
	t.Helper()
	srv := NewServer(t)
	allOpts := append([]plugin.AppOption{
		plugin.WithDataDir(t.TempDir()),
		plugin.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
	}, opts...)
	app := plugin.New(srv, allOpts...)
	t.Cleanup(app.Disable)
	return app
}

// NewWorld creates a synchronous, silent in-memory *world.World with automatic
// cleanup on t. Advance simulation ticks deterministically with w.AdvanceTick().
func NewWorld(t testing.TB, opts ...func(*world.Config)) *world.World {
	t.Helper()
	world.DefaultBlockRegistry.Finalize()
	cfg := world.Config{
		Log:             slog.New(slog.NewTextHandler(io.Discard, nil)),
		Synchronous:     true,
		RandomTickSpeed: -1,
		Generator:       world.NopGenerator{},
		Provider:        world.NopProvider{},
		Blocks:          world.DefaultBlockRegistry,
		Entities:        entity.DefaultRegistry,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	w := cfg.New()
	t.Cleanup(func() { _ = w.Close() })
	return w
}

// FakePlayer wraps a simulated *player.Player spawned in a test world alongside
// its *clienttest.Recorder for inspecting sent packets, messages, and HUD state.
type FakePlayer struct {
	t        testing.TB
	world    *world.World
	handle   *world.EntityHandle
	recorder *clienttest.Recorder
	id       uuid.UUID
	name     string
}

// SpawnPlayer spawns a simulated player named name into w (defaulting to
// {0.5, 64, 0.5} when pos is omitted).
func SpawnPlayer(t testing.TB, w *world.World, name string, pos ...mgl64.Vec3) *FakePlayer {
	t.Helper()
	spawnPos := mgl64.Vec3{0.5, 64, 0.5}
	if len(pos) > 0 {
		spawnPos = pos[0]
	}
	rec := clienttest.New()
	id := uuid.New()
	var handle *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		h := world.EntitySpawnOpts{
			Position: spawnPos,
			ID:       id,
			NameTag:  name,
		}.New(player.Type, player.Config{
			Client:   rec,
			UUID:     id,
			Name:     name,
			Position: spawnPos,
			Skin:     skin.New(64, 64),
			GameMode: world.GameModeSurvival,
		})
		p := tx.AddEntity(h).(*player.Player)
		handle = p.H()
	})
	return &FakePlayer{
		t:        t,
		world:    w,
		handle:   handle,
		recorder: rec,
		id:       id,
		name:     name,
	}
}

// Handle returns the player's *world.EntityHandle.
func (f *FakePlayer) Handle() *world.EntityHandle { return f.handle }

// Recorder returns the underlying *clienttest.Recorder capturing outbound UI,
// chat, HUD, and entity events for this player.
func (f *FakePlayer) Recorder() *clienttest.Recorder { return f.recorder }

// UUID returns the simulated player's UUID.
func (f *FakePlayer) UUID() uuid.UUID { return f.id }

// Name returns the simulated player's display name.
func (f *FakePlayer) Name() string { return f.name }

// Run executes fn synchronously inside the player's world transaction with the
// resolved *player.Player.
func (f *FakePlayer) Run(fn func(p *player.Player, tx *world.Tx)) {
	f.t.Helper()
	<-f.world.Exec(func(tx *world.Tx) {
		ent, ok := f.handle.Entity(tx)
		if !ok {
			f.t.Fatalf("sdktest: player %q no longer in world", f.name)
		}
		fn(ent.(*player.Player), tx)
	})
}
