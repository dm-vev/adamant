package clienttest_test

import (
	"context"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/client/clienttest"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/debug"
	"github.com/df-mc/dragonfly/server/player/hud"
	"github.com/df-mc/dragonfly/server/player/input"
	"github.com/df-mc/dragonfly/server/text"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

func TestRecorderRecordsCallsInOrder(t *testing.T) {
	r := clienttest.New()
	var c client.Client = r
	hello := text.Literal("hello")
	c.SendMessage(hello)
	c.SendTip(text.Literal("tip"))
	c.ViewTime(42)
	c.SendHealth(10, 20, 2)

	calls := r.Calls()
	var methods []string
	for _, call := range calls {
		methods = append(methods, call.Method)
	}
	if want := []string{"SendMessage", "SendTip", "ViewTime", "SendHealth"}; !slices.Equal(methods, want) {
		t.Fatalf("methods = %v, want %v", methods, want)
	}
	if got := calls[0].Args; len(got) != 1 || !text.Equal(got[0].(text.Text), hello) {
		t.Errorf("SendMessage args = %v, want [hello]", got)
	}
	if got := calls[3].Args; !slices.Equal(got, []any{10.0, 20.0, 2.0}) {
		t.Errorf("SendHealth args = %v, want [10 20 2]", got)
	}
	if got := r.CallsTo("SendTip"); len(got) != 1 {
		t.Errorf("CallsTo(SendTip) = %v, want one call", got)
	}

	r.Reset()
	if got := r.Calls(); len(got) != 0 {
		t.Errorf("Calls() after Reset = %v, want none", got)
	}
}

func TestRecorderQueriesAgreeWithState(t *testing.T) {
	r := clienttest.New()
	r.Answers = clienttest.Answers{
		Profile:      client.Profile{DeviceID: "device"},
		Latency:      time.Second,
		ChunkRadius:  8,
		ClientMoving: true,
		LastActivity: time.Unix(5, 0),
	}
	if r.Profile().DeviceID != "device" || r.Latency() != time.Second || r.ChunkRadius() != 8 || !r.ClientMoving() || !r.LastActivity().Equal(time.Unix(5, 0)) {
		t.Error("queries do not return the Answers")
	}
	r.HideHudElement(hud.Health())
	if !r.HudElementHidden(hud.Health()) {
		t.Error("HudElementHidden after HideHudElement = false")
	}
	r.ShowHudElement(hud.Health())
	if r.HudElementHidden(hud.Health()) {
		t.Error("HudElementHidden after ShowHudElement = true")
	}

	r.LockInput(input.Camera())
	r.LockInput(input.Jump())
	if !r.InputLocked(input.Camera()) || !r.InputLocked(input.Jump()) {
		t.Error("InputLocked after LockInput = false")
	}
	r.UnlockInput(input.Camera())
	if r.InputLocked(input.Camera()) || !r.InputLocked(input.Jump()) {
		t.Error("UnlockInput did not unlock only its input")
	}
	r.ClearInputLocks()
	if r.InputLocked(input.Jump()) {
		t.Error("InputLocked after ClearInputLocks = true")
	}

	a, b := &debug.Line{}, &debug.Box{}
	r.AddDebugShape(b)
	r.AddDebugShape(a)
	if got := r.VisibleDebugShapes(); len(got) != 2 {
		t.Errorf("VisibleDebugShapes() = %v, want two shapes", got)
	}
	r.RemoveDebugShape(a)
	if got := r.VisibleDebugShapes(); len(got) != 1 || got[0] != debug.Shape(b) {
		t.Errorf("VisibleDebugShapes() after RemoveDebugShape = %v, want [box]", got)
	}
	r.RemoveAllDebugShapes()
	if got := r.VisibleDebugShapes(); len(got) != 0 {
		t.Errorf("VisibleDebugShapes() after RemoveAllDebugShapes = %v, want none", got)
	}

	if r.ViewLayer() == nil || r.ViewLayer() != r.ViewLayer() { //nolint:staticcheck // SA4000: two calls must return the same layer.
		t.Error("ViewLayer() is not a layer of the Recorder's own")
	}
}

// Close only records for a Recorder of no Game: the player removes itself
// and closes its handle as it leaves.
func TestRecorderCloseOnlyRecords(t *testing.T) {
	w := world.Config{Log: slog.New(slog.DiscardHandler)}.New()
	t.Cleanup(func() { _ = w.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r := clienttest.New()
	pos := mgl64.Vec3{0.5, 64, 0.5}
	handle := world.EntitySpawnOpts{Position: pos, ID: uuid.New()}.New(player.Type, player.Config{Position: pos})
	if err := w.Do(func(tx *world.Tx) {
		tx.AddEntity(handle)
		r.Close(tx)
		r.Close(nil)
		if _, ok := handle.Entity(tx); !ok {
			t.Error("Close removed the player from the world")
		}
	}).Wait(ctx); err != nil {
		t.Fatal(err)
	}
	if handle.Closed() {
		t.Error("Close closed the handle of the player")
	}
	if got := r.CallsTo("Close"); len(got) != 2 {
		t.Errorf("CallsTo(Close) = %d calls, want 2", len(got))
	}
}
