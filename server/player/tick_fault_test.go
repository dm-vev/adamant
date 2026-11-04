package player

import (
	"fmt"
	"log/slog"
	"sync/atomic"
	"testing"

	"github.com/df-mc/dragonfly/server/client/clienttest"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// tickPanicClient is a client whose HUD updates, sent in every tick of its
// player, panic once armed.
type tickPanicClient struct {
	*clienttest.Recorder
	armed *atomic.Bool
}

func (c tickPanicClient) SendHudUpdates() {
	if c.armed.Load() {
		panic("test: player tick")
	}
	c.Recorder.SendHudUpdates()
}

// panicController is a controller that panics.
type panicController struct{}

func (panicController) Control(*world.Tx, world.Entity) { panic("test: controller") }

// tickFaultPlayer returns a synchronous world reporting its faults to
// faults, with a player in it whose client is client, and counts the calls of
// the Left hook of the player, by which the server saves it, in left.
func tickFaultPlayer(t *testing.T, client tickPanicClient, faults *[]world.Fault, left *int) (*world.World, *world.EntityHandle) {
	t.Helper()
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Log: slog.New(slog.DiscardHandler), Entities: entity.DefaultRegistry, Fault: func(f world.Fault) {
		*faults = append(*faults, f)
	}}.New()
	loader := world.NewLoader(2, w, clienttest.New())
	t.Cleanup(func() {
		<-w.Exec(func(tx *world.Tx) { loader.Close(tx) })
		_ = w.Close()
	})
	pos := mgl64.Vec3{0.5, 100, 0.5}
	<-w.Exec(func(tx *world.Tx) {
		loader.Move(tx, pos)
		loader.Load(tx, 25)
	})
	conf := Config{Name: "Alex", Position: pos, GameMode: world.GameModeSurvival, Client: client, Left: func(*world.Tx, *Player) { *left++ }}
	handle := world.EntitySpawnOpts{Position: pos, ID: uuid.New()}.New(Type, conf)
	<-w.Exec(func(tx *world.Tx) { tx.AddEntity(handle) })
	return w, handle
}

// brief returns the kind, type, value and count of each fault.
func brief(faults []world.Fault) []string {
	out := make([]string, len(faults))
	for i, f := range faults {
		out[i] = fmt.Sprintf("%v %v %v count=%d quarantined=%v", f.Kind, f.Type, f.Value, f.Count, f.Quarantined)
	}
	return out
}

// advance ticks w, failing the test if the tick panicked.
func advance(t *testing.T, w *world.World) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("the tick panicked: %v", r)
		}
	}()
	w.AdvanceTick()
}

// checkDisconnected checks that the player of handle was disconnected with
// an internal error once, left the world, and was saved once.
func checkDisconnected(t *testing.T, w *world.World, c *clienttest.Recorder, handle *world.EntityHandle, left int) {
	t.Helper()
	if calls := c.CallsTo("Disconnect"); len(calls) != 1 || argText(t, calls[0], 0).String() != "Internal server error" {
		t.Fatalf("the client was disconnected %d times, want once with an internal error", len(calls))
	}
	if left != 1 {
		t.Fatalf("the player left %d times, want once: leaving saves it", left)
	}
	<-w.Exec(func(tx *world.Tx) {
		if _, ok := handle.Entity(tx); ok {
			t.Error("the player is still in the world")
		}
	})
	if !handle.Closed() {
		t.Error("the handle of the player is open")
	}
}

// A player whose tick panics is disconnected with an internal error on the
// first panic, which saves it, rather than frozen.
func TestPlayerTickPanicDisconnects(t *testing.T) {
	var faults []world.Fault
	left := 0
	armed := new(atomic.Bool)
	c := tickPanicClient{Recorder: clienttest.New(), armed: armed}
	w, handle := tickFaultPlayer(t, c, &faults, &left)
	advance(t, w)
	armed.Store(true)
	advance(t, w)
	if len(faults) != 1 || faults[0].Kind != world.FaultPlayer || faults[0].Entity != handle || faults[0].Value != "test: player tick" || faults[0].Quarantined {
		t.Fatalf("faults = %v, want one of the player, not quarantined", brief(faults))
	}
	checkDisconnected(t, w, c.Recorder, handle, left)
	advance(t, w)
	if len(faults) != 1 {
		t.Fatalf("faults after the player left = %d, want 1", len(faults))
	}
}

// A player whose controller panics is disconnected as one whose tick does.
func TestPlayerControllerPanicDisconnects(t *testing.T) {
	var faults []world.Fault
	left := 0
	c := tickPanicClient{Recorder: clienttest.New(), armed: new(atomic.Bool)}
	w, handle := tickFaultPlayer(t, c, &faults, &left)
	handle.SetController(panicController{})
	advance(t, w)
	if len(faults) != 1 || faults[0].Kind != world.FaultPlayer || faults[0].Entity != handle || faults[0].Value != "test: controller" {
		t.Fatalf("faults = %v, want one of the player", brief(faults))
	}
	checkDisconnected(t, w, c.Recorder, handle, left)
}
