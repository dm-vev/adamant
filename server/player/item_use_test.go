package player

import (
	"context"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

func TestConsumableCompletesFromTicks(t *testing.T) {
	for _, test := range []struct {
		name     string
		repeated bool
	}{
		{name: "missing completion packet"},
		{name: "repeated use packets", repeated: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			withItemUsePlayer(t, Config{Food: 10}, func(_ *world.Tx, p *Player) {
				p.SetHeldItems(item.NewStack(item.Apple{}, 2), item.Stack{})
				p.UseItem()
				for tick := int64(1); tick <= 33; tick++ {
					if test.repeated {
						p.UseItem()
					}
					if p.UsingItem() {
						p.tickItemUse(tick)
					}
				}

				held, _ := p.HeldItems()
				if held.Count() != 1 || p.Food() != 14 || p.UsingItem() {
					t.Fatalf("unexpected completed consumption: held=%v food=%v using=%v", held.Count(), p.Food(), p.UsingItem())
				}
				p.UseItem()
				p.UseItem()
				if held, _ = p.HeldItems(); held.Count() != 1 || p.UsingItem() {
					t.Fatalf("stale completion packets restarted consumption: held=%v using=%v", held.Count(), p.UsingItem())
				}
			})
		})
	}
}

func TestConsumableCancellationStopsUse(t *testing.T) {
	withItemUsePlayer(t, Config{Food: 10}, func(_ *world.Tx, p *Player) {
		h := &cancelConsumeHandler{}
		p.h = h
		p.SetHeldItems(item.NewStack(item.Apple{}, 1), item.Stack{})
		p.UseItem()
		for tick := int64(1); tick <= 40 && p.UsingItem(); tick++ {
			p.tickItemUse(tick)
		}

		held, _ := p.HeldItems()
		if h.calls != 1 || held.Count() != 1 || p.Food() != 10 || p.UsingItem() {
			t.Fatalf("cancelled consumption changed state: calls=%v held=%v food=%v using=%v", h.calls, held.Count(), p.Food(), p.UsingItem())
		}
	})
}

func TestItemUseCancelledByHeldItemChanges(t *testing.T) {
	t.Run("slot switch", func(t *testing.T) {
		withItemUsePlayer(t, Config{Food: 10}, func(_ *world.Tx, p *Player) {
			p.SetHeldItems(item.NewStack(item.Apple{}, 1), item.Stack{})
			_ = p.Inventory().SetItem(1, item.NewStack(item.Apple{}, 1))
			p.UseItem()
			if err := p.SetHeldSlot(1); err != nil {
				t.Fatal(err)
			}
			if p.UsingItem() {
				t.Fatal("slot switch did not cancel item use")
			}
		})
	})

	t.Run("stack state", func(t *testing.T) {
		withItemUsePlayer(t, Config{Food: 10}, func(_ *world.Tx, p *Player) {
			p.SetHeldItems(item.NewStack(item.Apple{}, 2), item.Stack{})
			p.UseItem()
			p.SetHeldItems(item.NewStack(item.Apple{}, 1), item.Stack{})
			p.tickItemUse(1)
			if p.UsingItem() {
				t.Fatal("held stack change did not cancel item use")
			}
		})
	})
}

func TestChargeableCompletesFromTicks(t *testing.T) {
	withItemUsePlayer(t, Config{}, func(_ *world.Tx, p *Player) {
		p.SetHeldItems(item.NewStack(item.Crossbow{}, 1), item.Stack{})
		_ = p.Inventory().SetItem(1, item.NewStack(item.Arrow{}, 2))
		p.UseItem()
		for tick := int64(1); tick <= 25; tick++ {
			p.UseItem()
			p.tickItemUse(tick)
		}

		held, _ := p.HeldItems()
		crossbow := held.Item().(item.Crossbow)
		arrows, _ := p.Inventory().Item(1)
		if crossbow.Item.Empty() || arrows.Count() != 1 || p.UsingItem() {
			t.Fatalf("charge did not complete once: loaded=%v arrows=%v using=%v", !crossbow.Item.Empty(), arrows.Count(), p.UsingItem())
		}
		p.UseItem()
		p.UseItem()
		held, _ = p.HeldItems()
		if held.Item().(item.Crossbow).Item.Empty() {
			t.Fatal("stale use packet released completed crossbow")
		}
	})
}

func TestReleasableRequiresActiveUseAndReleasesOnce(t *testing.T) {
	withItemUsePlayer(t, Config{}, func(_ *world.Tx, p *Player) {
		p.SetHeldItems(item.NewStack(item.Bow{}, 1), item.Stack{})
		_ = p.Inventory().SetItem(1, item.NewStack(item.Arrow{}, 2))
		p.ReleaseItem()
		if arrows, _ := p.Inventory().Item(1); arrows.Count() != 2 {
			t.Fatal("release without active use consumed an arrow")
		}

		probe := &releasableProbe{}
		p.SetHeldItems(item.NewStack(probe, 1), item.Stack{})
		p.UseItem()
		for tick := int64(1); tick <= 3; tick++ {
			p.tickItemUse(tick)
		}
		p.ReleaseItem()
		p.ReleaseItem()
		if probe.releases != 1 || probe.duration < 150*time.Millisecond {
			t.Fatalf("item release was not stable: releases=%v duration=%v", probe.releases, probe.duration)
		}
	})
}

type cancelConsumeHandler struct {
	NopHandler
	calls int
}

func (h *cancelConsumeHandler) HandleItemConsume(ctx *Context, _ item.Stack) {
	h.calls++
	ctx.Cancel()
}

type releasableProbe struct {
	releases int
	duration time.Duration
}

func (p *releasableProbe) Release(_ item.Releaser, _ *world.Tx, _ *item.UseContext, duration time.Duration) {
	p.releases++
	p.duration = duration
}

func (p *releasableProbe) Requirements() []item.Stack { return nil }
func (*releasableProbe) EncodeItem() (string, int16)  { return "test:releasable", 0 }

func withItemUsePlayer(t *testing.T, cfg Config, f func(tx *world.Tx, p *Player)) {
	t.Helper()
	w := world.New()
	t.Cleanup(func() { _ = w.Close() })
	pos := mgl64.Vec3{0.5, 64, 0.5}
	cfg.Position = pos
	handle := world.EntitySpawnOpts{Position: pos, ID: uuid.New()}.New(Type, cfg)
	if err := w.Do(func(tx *world.Tx) {
		f(tx, tx.AddEntity(handle).(*Player))
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}
