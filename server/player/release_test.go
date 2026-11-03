package player

import (
	"context"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/client/clienttest"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
)

// A release that consumes nothing has the client, which predicted the
// release, shown the inventories again; one that consumes an arrow does
// not.
func TestReleaseItemResyncsOnlyWhenNothingWasConsumed(t *testing.T) {
	tests := []struct {
		name              string
		arrows            int
		useDuration       time.Duration
		handler           Handler
		wantResyncs       int
		wantArrowConsumed bool
	}{
		{name: "cancelled", arrows: 1, useDuration: time.Second, handler: cancelReleaseHandler{}, wantResyncs: 1},
		{name: "too short", arrows: 1, wantResyncs: 1},
		{name: "missing ammo", useDuration: time.Second, wantResyncs: 1},
		{name: "consumed", arrows: 1, useDuration: time.Second, wantArrowConsumed: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c := clienttest.New()
			w := world.Config{Entities: entity.DefaultRegistry}.New()
			t.Cleanup(func() { _ = w.Close() })
			inv := inventory.New(36, nil)
			_ = inv.SetItem(0, item.NewStack(item.Bow{}, 1))
			if test.arrows > 0 {
				_ = inv.SetItem(1, item.NewStack(item.Arrow{}, test.arrows))
			}
			handle := world.EntitySpawnOpts{ID: uuid.New()}.New(Type, Config{Client: c, Inventory: inv})

			if err := w.Do(func(tx *world.Tx) {
				tx.AddEntity(handle)
			}).Wait(context.Background()); err != nil {
				t.Fatalf("add player: %v", err)
			}
			c.Reset()

			if err := w.Do(func(tx *world.Tx) {
				entity, ok := handle.Entity(tx)
				if !ok {
					t.Fatal("player is not in world")
				}
				p := entity.(*Player)
				held, _ := p.HeldItems()
				p.startItemUse(held)
				p.usingSince = time.Now().Add(-test.useDuration)
				if test.handler != nil {
					p.Handle(test.handler)
				}
				p.ReleaseItem()
			}).Wait(context.Background()); err != nil {
				t.Fatalf("release item: %v", err)
			}

			if got := len(c.CallsTo("ResyncInventory")); got != test.wantResyncs {
				t.Errorf("ResyncInventory calls = %d, want %d", got, test.wantResyncs)
			}
			if test.wantArrowConsumed {
				arrow, _ := inv.Item(1)
				if !arrow.Empty() {
					t.Errorf("arrow was not consumed: %v", arrow)
				}
			}
		})
	}
}

type cancelReleaseHandler struct{ NopHandler }

func (cancelReleaseHandler) HandleItemRelease(ctx *Context, _ item.Stack, _ time.Duration) {
	ctx.Cancel()
}
