package server

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/internal/faultcheck"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/identity"
	"github.com/df-mc/dragonfly/server/version"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// storeCountingProvider is a world provider counting the columns stored.
type storeCountingProvider struct {
	world.NopProvider
	stores atomic.Int32
}

func (p *storeCountingProvider) StoreColumn(pos world.ChunkPos, dim world.Dimension, col *chunk.Column) error {
	p.stores.Add(1)
	return p.NopProvider.StoreColumn(pos, dim, col)
}

func TestCrashReportAndOrderlyStop(t *testing.T) {
	dir := t.TempDir()
	players, worlds := &joinSaveProvider{}, &storeCountingProvider{}
	// Once armed, the entity collisions of the overworld panic every tick,
	// which fails it after 20 ticks.
	var armed atomic.Bool
	entities := *entity.DefaultRegistry.Config()
	collide := entities.EntityCollisions
	entities.EntityCollisions = func(tx *world.Tx) {
		if armed.Load() {
			panic(faultcheck.Expected{Why: "test: collisions"})
		}
		if collide != nil {
			collide(tx)
		}
	}
	jt := newJoinTest(t, players, Config{
		CrashReports:  filepath.Join(dir, "crash-reports"),
		WorldProvider: worlds,
		Entities:      entities.New(entity.DefaultRegistry.Types()),
	})
	jt.srv.Listen()
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		for p := range jt.srv.Accept() {
			jt.spawned <- p.H()
		}
	}()
	jt.join(t, joinLogin("Alice", ""))
	// The client of the test views no chunks: a loader of its own has the
	// world tick as with a client that does.
	w := jt.srv.World()
	<-w.Exec(func(tx *world.Tx) { world.NewLoader(1, w, world.NopViewer{}).Move(tx, mgl64.Vec3{}) })
	if n := worlds.stores.Load(); n != 0 {
		t.Fatalf("%d columns stored before the crash", n)
	}

	armed.Store(true)
	select {
	case <-stopped:
	case <-time.After(30 * time.Second):
		reports, _ := filepath.Glob(filepath.Join(dir, "crash-reports", "*"))
		var stuck []string
		for _, g := range strings.Split(string(allStacks()), "\n\n") {
			if strings.Contains(g, "server.(*Server).close") || strings.Contains(g, "handleTransactions") {
				stuck = append(stuck, g)
			}
		}
		t.Fatalf("the server did not stop after its world failed; reports %v:\n%s", reports, strings.Join(stuck, "\n\n"))
	}

	reports, _ := filepath.Glob(filepath.Join(dir, "crash-reports", "crash-*-server.txt"))
	if len(reports) != 1 {
		t.Fatalf("crash reports = %v, want one", reports)
	}
	data, err := os.ReadFile(reports[0])
	if err != nil {
		t.Fatal(err)
	}
	report := string(data)
	for _, want := range []string{
		"---- Minecraft Crash Report ----",
		"Description: Exception ticking world",
		"panic: test: collisions",
		"Kind: fatal",
		"Blamed on: entity collisions",
		"Dimension: overworld",
		"Minecraft Version: " + version.Minecraft,
		"crash_test.go",
		"-- Goroutines --",
		"goroutine ",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("the crash report lacks %q:\n%s", want, report[:min(len(report), 4000)])
		}
	}
	// The stop was in order: Alice was saved as she left, and the world
	// stored the columns she loaded as it closed.
	if got := players.saved(); !slices.Equal(got, []uuid.UUID{identity.OfflineUUID("Alice")}) {
		t.Errorf("players saved %v, want Alice once", got)
	}
	if worlds.stores.Load() == 0 {
		t.Error("the failed world stored no column as it closed")
	}
}
