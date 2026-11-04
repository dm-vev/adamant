package server

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
)

// failing returns the names of the checks of r that failed.
func failing(r HealthReport) []string {
	var names []string
	for _, c := range r.Checks {
		if !c.OK {
			names = append(names, c.Name)
		}
	}
	return names
}

// TestReadiness checks the readiness of a server through its life: not
// ready while starting, ready once listening, not ready without a listener
// accepting, with a checkpoint too old unless saving is off, nor once
// closing; live throughout.
func TestReadiness(t *testing.T) {
	jt := newJoinTest(t, player.NopProvider{}, Config{SaveInterval: time.Minute})
	o := HealthOptions{}
	if r := jt.srv.Ready(o); r.OK || !strings.Contains(r.String(false), "[-]server/listening starting") {
		t.Fatalf("a server not listening is ready:\n%s", r.String(true))
	}
	if r := jt.srv.Live(o); !r.OK || r.String(false) != "ok\n" {
		t.Fatalf("a server starting is not live:\n%s", r.String(true))
	}
	jt.start()
	if r := jt.srv.Ready(o); !r.OK {
		t.Fatalf("a server listening is not ready:\n%s", r.String(true))
	}
	if got := jt.srv.Ready(o).String(true); !strings.Contains(got, "[+]listener/0\n") || !strings.Contains(got, "[+]world/overworld/ticked\n") || !strings.Contains(got, "[+]checkpoint\n") {
		t.Errorf("checks of a server ready:\n%s", got)
	}

	// Three minutes later, with no checkpoint since the start: 2 intervals
	// and a minute have passed. The world ticks still, read at the real
	// time.
	later := time.Now().Add(3*time.Minute + time.Second)
	jt.srv.clock = func() time.Time { return later }
	if got := failing(jt.srv.Ready(o)); !slices.Contains(got, "checkpoint") {
		t.Errorf("a checkpoint 3 minutes old with an interval of 1: failing %v", got)
	}
	jt.srv.SaveEnabled(false)
	if got := failing(jt.srv.Ready(o)); slices.Contains(got, "checkpoint") {
		t.Errorf("with saving off, the checkpoint fails: %v", got)
	}
	jt.srv.SaveEnabled(true)
	jt.srv.checkpoints.lastSuccess.Store(later.UnixNano())
	if got := failing(jt.srv.Ready(o)); slices.Contains(got, "checkpoint") {
		t.Errorf("a checkpoint just saved fails: %v", got)
	}
	jt.srv.clock = nil

	_ = jt.l.Close()
	for deadline := time.Now().Add(5 * time.Second); !slices.Contains(failing(jt.srv.Ready(o)), "listener/0"); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("a listener closed is accepting:\n%s", jt.srv.Ready(o).String(true))
		}
	}
	if err := jt.srv.Close(); err != nil {
		t.Fatal(err)
	}
	if got := failing(jt.srv.Ready(o)); !slices.Contains(got, "server/listening") {
		t.Errorf("a server closed is listening: %v", got)
	}
	if r := jt.srv.Live(o); !r.OK {
		t.Errorf("a server closed is not live:\n%s", r.String(true))
	}
}

// TestProbesNeverWaitForOwners holds the owner of the world of a server and
// checks that the probes answer at once: live until the tick waiting for it
// is older than LiveTimeout, and not ready once the last tick is older than
// ReadyTickAge.
func TestProbesNeverWaitForOwners(t *testing.T) {
	jt := newJoinTest(t, player.NopProvider{}, Config{})
	jt.start()
	t.Cleanup(func() { _ = jt.srv.Close() })
	w := jt.srv.World()
	release, held := make(chan struct{}), make(chan struct{})
	w.Do(func(*world.Tx) {
		close(held)
		<-release
	})
	defer close(release)
	<-held
	for deadline := time.Now().Add(5 * time.Second); !w.TickState().Running; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("no tick waits for the owner held")
		}
	}
	o := HealthOptions{LiveTimeout: 2 * time.Minute, ReadyTickAge: 5 * time.Second}
	probe := func(name string, f func(HealthOptions) HealthReport) HealthReport {
		start := time.Now()
		r := f(o)
		if took := time.Since(start); took > 100*time.Millisecond {
			t.Errorf("%s took %v with the owner held", name, took)
		}
		return r
	}
	if r := probe("Live", jt.srv.Live); !r.OK {
		t.Errorf("a world held for a moment is not live:\n%s", r.String(true))
	}
	stuck := time.Now().Add(2*time.Minute + time.Second)
	jt.srv.clock = func() time.Time { return stuck }
	if r := probe("Live", jt.srv.Live); r.OK || !slices.Contains(failing(r), "world/overworld/ticking") {
		t.Errorf("a world held for 2 minutes is live:\n%s", r.String(true))
	}
	if r := probe("Ready", jt.srv.Ready); !slices.Contains(failing(r), "world/overworld/ticked") {
		t.Errorf("a world held for 2 minutes is ready:\n%s", r.String(true))
	}
	jt.srv.clock = nil
}

// slowPlayers is a player provider whose Save waits for release.
type slowPlayers struct {
	player.NopProvider
	saving  chan struct{}
	release chan struct{}
}

func (p *slowPlayers) Save(uuid.UUID, player.Config, *world.World) error {
	select {
	case p.saving <- struct{}{}:
	default:
	}
	<-p.release
	return nil
}

// TestLivenessHoldsWhileClosing closes a server whose players save slowly:
// it is not ready from the start of the close, and live throughout.
func TestLivenessHoldsWhileClosing(t *testing.T) {
	provider := &slowPlayers{saving: make(chan struct{}, 1), release: make(chan struct{})}
	jt := newJoinTest(t, provider, Config{})
	jt.start()
	jt.join(t, joinLogin("Alice", ""))
	closed := make(chan struct{})
	go func() {
		_ = jt.srv.Close()
		close(closed)
	}()
	select {
	case <-provider.saving:
	case <-time.After(10 * time.Second):
		close(provider.release)
		t.Fatal("closing the server saved no player")
	}
	o := HealthOptions{}
	if got := failing(jt.srv.Ready(o)); !slices.Contains(got, "server/listening") {
		t.Errorf("a server closing is ready: failing %v", got)
	}
	if r := jt.srv.Live(o); !r.OK {
		t.Errorf("a server closing is not live:\n%s", r.String(true))
	}
	close(provider.release)
	<-closed
}
