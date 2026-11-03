package player

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// deathAnimationWait outlasts the 1.1s death animation after which
// finishDying runs.
const deathAnimationWait = 1500 * time.Millisecond

// A player without a client has nobody to press respawn, so once its death
// animation ends it must leave the world.
func TestPlayerWithoutClientIsRemovedAfterDeath(t *testing.T) {
	w := world.Config{Log: slog.New(slog.DiscardHandler)}.New()
	t.Cleanup(func() { _ = w.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pos := mgl64.Vec3{0.5, 64, 0.5}
	handle := world.EntitySpawnOpts{Position: pos, ID: uuid.New()}.New(Type, Config{Position: pos, GameMode: world.GameModeSurvival})
	quits, respawns := 0, 0
	if err := w.Do(func(tx *world.Tx) {
		p := tx.AddEntity(handle).(*Player)
		p.Handle(deathCloseHandler{quits: &quits, respawns: &respawns})
		p.Hurt(1000, entity.VoidDamageSource{})
		if !p.Dead() {
			t.Error("player survived lethal damage")
		}
	}).Wait(ctx); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(deathAnimationWait + 2*time.Second)
	for {
		err := handle.Do(func(*world.Tx, world.Entity) {}).Wait(ctx)
		if errors.Is(err, world.ErrEntityClosed) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("dead player without a client is still in the world (err=%v)", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := w.Do(func(tx *world.Tx) {
		for e := range tx.Players() {
			if e.H() == handle {
				t.Error("world still lists the player")
			}
		}
	}).Wait(ctx); err != nil {
		t.Fatal(err)
	}
	if quits != 1 || respawns != 0 {
		t.Errorf("quits=%d respawns=%d, want 1 and 0", quits, respawns)
	}
}

type deathCloseHandler struct {
	NopHandler
	quits, respawns *int
}

func (h deathCloseHandler) HandleQuit(*Player) { *h.quits++ }

func (h deathCloseHandler) HandleRespawn(*Player, *mgl64.Vec3, **world.World) { *h.respawns++ }
