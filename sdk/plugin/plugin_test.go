package plugin_test

import (
	"io"
	"log/slog"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/sdk/ecs"
	"github.com/df-mc/dragonfly/sdk/event"
	"github.com/df-mc/dragonfly/sdk/plugin"
	"github.com/df-mc/dragonfly/sdk/schedule"
	"github.com/df-mc/dragonfly/sdk/worlds"
	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/client/clienttest"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/skin"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

func newTestServer(t *testing.T) *server.Server {
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

func newSyncWorld(t *testing.T) *world.World {
	t.Helper()
	world.DefaultBlockRegistry.Finalize()
	w := world.Config{
		Log:             slog.New(slog.NewTextHandler(io.Discard, nil)),
		Synchronous:     true,
		RandomTickSpeed: -1,
		Generator:       world.NopGenerator{},
		Provider:        world.NopProvider{},
		Blocks:          world.DefaultBlockRegistry,
		Entities:        entity.DefaultRegistry,
	}.New()
	t.Cleanup(func() { _ = w.Close() })
	return w
}

func TestPluginLifecycleServicesAndStorage(t *testing.T) {
	srv := newTestServer(t)
	dataDir := t.TempDir()
	app := plugin.New(srv, plugin.WithDataDir(dataDir), plugin.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))

	type greeter interface{ Greet() string }
	type impl struct{ msg string }
	var _ = impl{}
	var order []string

	type svcStruct struct{ Tag string }

	p1 := plugin.Func("alpha", func(ctx *plugin.Context) {
		order = append(order, "enable:alpha")
		plugin.Provide(ctx.App(), &svcStruct{Tag: "v1"})
		ctx.OnDisable(func() { order = append(order, "cleanup:alpha:1") })
		ctx.OnDisable(func() { order = append(order, "cleanup:alpha:2") })

		// Test raw file storage & path traversal protection.
		if err := ctx.WriteFile("sub/hello.txt", []byte("world")); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		got, err := ctx.ReadFile("sub/hello.txt")
		if err != nil || string(got) != "world" {
			t.Fatalf("ReadFile = (%q, %v)", got, err)
		}
		if err := ctx.WriteFile("../escape.txt", []byte("bad")); err == nil {
			t.Fatal("expected error for path traversal")
		}
		if err := ctx.DeleteFile("sub/hello.txt"); err != nil {
			t.Fatalf("DeleteFile: %v", err)
		}

		// Test JSON & TOML persistence.
		type cfg struct {
			Motd  string `json:"motd" toml:"motd"`
			Limit int    `json:"limit" toml:"limit"`
		}
		def := cfg{Motd: "Welcome", Limit: 42}
		loaded, err := plugin.LoadOrDefaultJSON(ctx, "cfg.json", def)
		if err != nil || loaded != def {
			t.Fatalf("LoadOrDefaultJSON = (%+v, %v)", loaded, err)
		}
		loaded.Limit = 99
		if err := plugin.SaveJSON(ctx, "cfg.json", loaded); err != nil {
			t.Fatalf("SaveJSON: %v", err)
		}
		reread, err := plugin.LoadJSON[cfg](ctx, "cfg.json")
		if err != nil || reread.Limit != 99 {
			t.Fatalf("LoadJSON = (%+v, %v)", reread, err)
		}

		loadedToml, err := plugin.LoadOrDefaultTOML(ctx, "cfg.toml", def)
		if err != nil || loadedToml != def {
			t.Fatalf("LoadOrDefaultTOML = (%+v, %v)", loadedToml, err)
		}
		loadedToml.Motd = "Updated"
		if err := plugin.SaveTOML(ctx, "cfg.toml", loadedToml); err != nil {
			t.Fatalf("SaveTOML: %v", err)
		}
		rereadToml, err := plugin.LoadTOML[cfg](ctx, "cfg.toml")
		if err != nil || rereadToml.Motd != "Updated" {
			t.Fatalf("LoadTOML = (%+v, %v)", rereadToml, err)
		}
	}, func(*plugin.Context) {
		order = append(order, "disable:alpha")
	})

	p2 := plugin.Func("beta", func(ctx *plugin.Context) {
		order = append(order, "enable:beta")
		svc, ok := plugin.Service[*svcStruct](ctx.App())
		if !ok || svc.Tag != "v1" {
			t.Fatalf("Service = (%+v, %v), want v1", svc, ok)
		}
		if plugin.MustService[*svcStruct](ctx.App()).Tag != "v1" {
			t.Fatal("MustService mismatch")
		}
		if _, ok := plugin.Service[greeter](ctx.App()); ok {
			t.Fatal("unexpected greeter service")
		}
	}, func(*plugin.Context) {
		order = append(order, "disable:beta")
	})

	app.Use(p1, p2)
	var names []string
	for _, p := range app.Plugins() {
		names = append(names, p.Name())
	}
	if !slices.Equal(names, []string{"alpha", "beta"}) {
		t.Fatalf("Plugins() = %v", names)
	}
	app.Disable()
	app.Disable() // idempotent

	want := []string{
		"enable:alpha",
		"enable:beta",
		"disable:beta",
		"disable:alpha",
		"cleanup:alpha:2",
		"cleanup:alpha:1",
	}
	if !slices.Equal(order, want) {
		t.Fatalf("lifecycle order = %v, want %v", order, want)
	}
}

func TestEventBusPriorityAndPluginAutoCleanup(t *testing.T) {
	srv := newTestServer(t)
	app := plugin.New(srv)

	type pingEvent struct {
		event.Cancellable
		Trace []string
	}

	var serverHits atomic.Int32
	app.Use(plugin.Func("listener", func(ctx *plugin.Context) {
		event.OnServer[pingEvent](ctx, event.Low, func(ev *pingEvent) {
			ev.Trace = append(ev.Trace, "low")
			serverHits.Add(1)
		})
		event.OnServer[pingEvent](ctx, event.High, func(ev *pingEvent) {
			ev.Trace = append(ev.Trace, "high")
			ev.Cancel()
		})
		event.OnServer[pingEvent](ctx, event.Monitor, func(ev *pingEvent) {
			ev.Trace = append(ev.Trace, "monitor")
		})
	}, nil))

	ev := &pingEvent{}
	if event.Emit(app.Events(), ev) {
		t.Fatal("expected cancelled event to return false")
	}
	if !slices.Equal(ev.Trace, []string{"low", "high", "monitor"}) {
		t.Fatalf("trace = %v", ev.Trace)
	}
	if !event.HasListeners[pingEvent](app.Events()) {
		t.Fatal("expected HasListeners true")
	}

	// Disabling the plugin must automatically unsubscribe all listeners.
	app.Disable()
	ev2 := &pingEvent{}
	if !event.Emit(app.Events(), ev2) {
		t.Fatal("expected uncancelled event after plugin disable")
	}
	if serverHits.Load() != 1 {
		t.Fatalf("serverHits = %d, want 1 after auto-unsubscribe", serverHits.Load())
	}
}

func TestECSComponentsAndScheduleAutoCancel(t *testing.T) {
	srv := newTestServer(t)
	app := plugin.New(srv)
	w := newSyncWorld(t)

	type teamData struct {
		Name  string
		Score int
	}
	teamKey := ecs.NewKey[teamData]("sdk_test:team", ecs.Codec(
		func(v teamData) map[string]any { return map[string]any{"name": v.Name, "score": int32(v.Score)} },
		func(m map[string]any) (teamData, bool) {
			n, _ := m["name"].(string)
			s, _ := m["score"].(int32)
			return teamData{Name: n, Score: int(s)}, true
		},
	))

	ecs.Set(w, teamKey, teamData{Name: "Red", Score: 10})
	ecs.Update(w, teamKey, func(d *teamData) { d.Score += 5 })
	if got, ok := ecs.Get(w, teamKey); !ok || got.Score != 15 || !ecs.Has(w, teamKey) {
		t.Fatalf("world ecs = (%+v, %v)", got, ok)
	}
	ecs.Delete(w, teamKey)
	if ecs.Has(w, teamKey) {
		t.Fatal("expected teamKey deleted from world")
	}

	<-w.Exec(func(tx *world.Tx) {
		chunkBag := ecs.OfChunk(tx, world.ChunkPos{0, 0})
		ecs.Set(chunkBag, teamKey, teamData{Name: "Blue", Score: 7})
		if got, ok := ecs.GetBag(chunkBag, teamKey); !ok || got.Name != "Blue" {
			t.Fatalf("chunk ecs = (%+v, %v)", got, ok)
		}
	})

	// Schedule tasks scoped to a plugin context.
	var afterCount, everyCount, sysCount atomic.Int32
	app.Use(plugin.Func("sched", func(ctx *plugin.Context) {
		schedule.System(ctx, w, schedule.PreEntities, func(_ *world.Tx, _ int64) {
			sysCount.Add(1)
		})
		schedule.After(ctx, w, 2, func(_ *world.Tx) {
			afterCount.Add(1)
		})
		schedule.Every(ctx, w, 2, func(_ *world.Tx) {
			everyCount.Add(1)
		})
		schedule.AfterDuration(ctx, w, 100*time.Millisecond, func(_ *world.Tx) {})
	}, nil))

	for range 4 {
		w.AdvanceTick()
	}
	if afterCount.Load() != 1 || everyCount.Load() != 2 || sysCount.Load() != 4 {
		t.Fatalf("ticks: after=%d every=%d sys=%d", afterCount.Load(), everyCount.Load(), sysCount.Load())
	}

	// Disabling the plugin must auto-cancel all scheduled tasks & systems.
	app.Disable()
	for range 4 {
		w.AdvanceTick()
	}
	if everyCount.Load() != 2 || sysCount.Load() != 4 {
		t.Fatalf("tasks kept firing after disable: every=%d sys=%d", everyCount.Load(), sysCount.Load())
	}
}

func TestWorldsManagerAndTransfer(t *testing.T) {
	srv := newTestServer(t)
	app := plugin.New(srv)

	var mgr *worlds.Manager
	var arena *world.World
	app.Use(plugin.Func("arenas", func(ctx *plugin.Context) {
		mgr = worlds.NewScopedManager(ctx)
		var err error
		arena, err = mgr.Open("arena_1", worlds.Synchronous(), worlds.RandomTickSpeed(-1))
		if err != nil {
			t.Fatalf("mgr.Open: %v", err)
		}
		if _, err := mgr.Open("arena_1"); err == nil {
			t.Fatal("expected duplicate world name error")
		}
	}, nil))

	if got, ok := mgr.ByName("arena_1"); !ok || got != arena {
		t.Fatalf("ByName(arena_1) = (%v, %v)", got, ok)
	}
	if _, ok := mgr.ByName("overworld"); !ok {
		t.Fatal("ByName(overworld) returned false")
	}
	if !slices.Equal(mgr.Names(), []string{"arena_1"}) {
		t.Fatalf("Names() = %v", mgr.Names())
	}

	// Test Transfer between two synchronous worlds.
	src := newSyncWorld(t)
	dst := newSyncWorld(t)
	var playerHandle *world.EntityHandle
	<-src.Exec(func(tx *world.Tx) {
		h := world.EntitySpawnOpts{Position: mgl64.Vec3{1, 64, 1}}.New(player.Type, player.Config{
			Client: clienttest.New(),
			UUID:   uuid.New(),
			Name:   "Traveler",
			Skin:   skin.New(64, 64),
		})
		p := tx.AddEntity(h).(*player.Player)
		playerHandle = p.H()
		if !worlds.Transfer(p, dst, mgl64.Vec3{10, 70, 20}, tx) {
			t.Fatal("worlds.Transfer returned false")
		}
	})
	<-dst.Exec(func(tx *world.Tx) {
		ent, ok := playerHandle.Entity(tx)
		if !ok {
			t.Fatal("player not found in destination world after Transfer")
		}
		if ent.Position() != (mgl64.Vec3{10, 70, 20}) {
			t.Fatalf("transferred position = %v, want {10 70 20}", ent.Position())
		}
	})

	app.Disable()
	if len(mgr.Names()) != 0 {
		t.Fatalf("expected worlds closed on disable, remaining: %v", mgr.Names())
	}
}
