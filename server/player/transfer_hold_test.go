package player

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/client/clienttest"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// awaitInWorld waits until the entity of h is in w and f holds for it there.
func awaitInWorld(t *testing.T, h *world.EntityHandle, w *world.World, f func(p *Player) bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		ok, err := world.CallEntity(context.Background(), h, func(tx *world.Tx, e world.Entity) (bool, error) {
			return tx.World() == w && f(e.(*Player)), nil
		})
		if err == nil && ok {
			return
		}
	}
	t.Fatalf("the player did not reach %v within 5s", w.Dimension())
}

// TestRespawnWaitsWhileTransfersAreHeld checks that a player respawning, or
// teleported to another world, while a checkpoint holds transfers out of its
// world stays as it is, and respawns or moves once the hold is lifted.
func TestRespawnWaitsWhileTransfersAreHeld(t *testing.T) {
	log := slog.New(slog.DiscardHandler)
	w := world.Config{Log: log}.New()
	nether := world.Config{Log: log, Dim: world.Nether}.New()
	t.Cleanup(func() {
		_ = w.Close()
		_ = nether.Close()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pos := mgl64.Vec3{0.5, 64, 0.5}
	cfg := Config{Client: clienttest.New(), Position: pos, GameMode: world.GameModeSurvival}
	handle := world.EntitySpawnOpts{Position: pos, ID: uuid.New()}.New(Type, cfg)
	quits, respawns := 0, 0
	if err := w.Do(func(tx *world.Tx) {
		p := tx.AddEntity(handle).(*Player)
		p.Handle(deathCloseHandler{quits: &quits, respawns: &respawns})
		p.Hurt(1000, entity.VoidDamageSource{})
	}).Wait(ctx); err != nil {
		t.Fatal(err)
	}

	release, err := world.HoldTransfers(ctx, []*world.World{w})
	if err != nil {
		t.Fatal(err)
	}
	if err := handle.Do(func(_ *world.Tx, e world.Entity) { e.(*Player).Respawn() }).Wait(ctx); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if err := handle.Do(func(tx *world.Tx, e world.Entity) {
		if p := e.(*Player); !p.Dead() || respawns != 0 || tx.World() != w {
			t.Errorf("while held: dead=%v respawns=%d, want still dead in its world, not respawned", p.Dead(), respawns)
		}
	}).Wait(ctx); err != nil {
		t.Fatal(err)
	}
	release()
	awaitInWorld(t, handle, w, func(p *Player) bool { return !p.Dead() && respawns == 1 })

	release, err = world.HoldTransfers(ctx, []*world.World{w})
	if err != nil {
		t.Fatal(err)
	}
	if err := handle.Do(func(tx *world.Tx, e world.Entity) {
		e.(*Player).TeleportToWorld(tx, nether, mgl64.Vec3{4.5, 70, 4.5})
	}).Wait(ctx); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if err := handle.Do(func(tx *world.Tx, _ world.Entity) {
		if tx.World() != w {
			t.Errorf("while held, the player teleported to %v", tx.World().Dimension())
		}
	}).Wait(ctx); err != nil {
		t.Fatal(err)
	}
	release()
	awaitInWorld(t, handle, nether, func(p *Player) bool { return p.Position() == mgl64.Vec3{4.5, 70, 4.5} })
	if quits != 0 {
		t.Errorf("the player quit %d times", quits)
	}
}
