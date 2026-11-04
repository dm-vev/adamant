package builtin

import (
	"context"
	"errors"
	"testing"

	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/client/clienttest"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// saveAllTestAdapter keeps the done function SaveAllFrom is passed instead
// of saving.
type saveAllTestAdapter struct {
	serverAdapter
	done func(tx *world.Tx, err error)
}

func (s *saveAllTestAdapter) SaveAllFrom(_ *world.Tx, done func(tx *world.Tx, err error)) {
	s.done = done
}

// outputs is a source that is no entity and records its outputs.
type outputs struct{ got []*cmd.Output }

func (o *outputs) Position() mgl64.Vec3              { return mgl64.Vec3{} }
func (o *outputs) SendCommandOutput(out *cmd.Output) { o.got = append(o.got, out) }

// TestSaveAllPrintsOnceSaved checks that /save-all tells its source it saved
// the world from the done function of SaveAllFrom, not from Run: a source
// that is no entity directly, and a player, valid only in the transaction of
// the command, found again by its handle in the transaction of done.
func TestSaveAllPrintsOnceSaved(t *testing.T) {
	srv := &saveAllTestAdapter{}
	c := saveAllCmd{adminPermission: adminPermission{srv}, srv: srv}

	src := &outputs{}
	c.Run(cmd.WithContext(src, cmd.ExecutionContext{}), &cmd.Output{}, nil)
	if srv.done == nil || len(src.got) != 0 {
		t.Fatalf("Run did not leave the output to done: done set %v, %d outputs", srv.done != nil, len(src.got))
	}
	srv.done(nil, errors.New("disk full"))
	if len(src.got) != 1 || src.got[0].ErrorCount() != 1 || src.got[0].Errors()[0].Error() != "disk full" {
		t.Fatalf("a failed save sent %v, want its error", src.got)
	}

	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	rec := clienttest.New()
	h := world.EntitySpawnOpts{}.New(player.Type, player.Config{Name: "Alice", Client: rec})
	srv.done = nil
	if err := w.Do(func(tx *world.Tx) {
		p := tx.AddEntity(h).(*player.Player)
		c.Run(p, &cmd.Output{}, tx)
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if srv.done == nil || len(rec.CallsTo("SendCommandOutput")) != 0 {
		t.Fatal("Run did not leave the output of the player to done")
	}
	if err := w.Do(func(tx *world.Tx) { srv.done(tx, nil) }).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	calls := rec.CallsTo("SendCommandOutput")
	if len(calls) != 1 {
		t.Fatalf("the player was sent %d outputs, want one", len(calls))
	}
	out := calls[0].Args[0].(client.CommandOutput)
	if len(out.Messages) != 1 || out.Messages[0].Text.Legacy() != "Saved the world." || !out.Messages[0].Success {
		t.Errorf("the player was sent %+v, want \"Saved the world.\"", out)
	}
}
