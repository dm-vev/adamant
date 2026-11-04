package player

import (
	"errors"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/client/clienttest"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/menu"
	"github.com/df-mc/dragonfly/server/player/anticheat"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestMenuClosesOutOfReach checks that a chest menu stays open within
// Java's 4.5 blocks and the buffer of 4 of its box, and closes 20 blocks
// away, as the server closes it, which the client is shown.
func TestMenuClosesOutOfReach(t *testing.T) {
	withBreaker(t, anticheat.Default(), func(tx *world.Tx, p *Player, r *clienttest.Recorder, h *violationRecorder, tick func(int)) {
		chest := cube.Pos{1, 100, 0}
		tx.SetBlock(chest, block.NewChest(), nil)
		p.OpenBlockContainer(chest, tx)
		p.data.Pos = mgl64.Vec3{9.5, 100, 0.5}
		tick(1)
		if p.Menu() == nil || len(h.violations) != 0 {
			t.Fatalf("8 blocks from the chest: menu %v, violations %+v", p.Menu(), h.violations)
		}
		p.data.Pos = mgl64.Vec3{21.5, 100, 0.5}
		tick(1)
		if p.Menu() != nil || h.count(anticheat.Menu) != 1 {
			t.Fatalf("20 blocks from the chest: menu %v, violations %+v", p.Menu(), h.violations)
		}
		closes := r.CallsTo("CloseMenu")
		if len(closes) != 1 || closes[0].Args[1] != false {
			t.Fatalf("closes %v, want one by the server", closes)
		}
	})
}

// TestMenuBoundsRequests checks that beyond 64 menu batches in a tick, the
// operations of a batch are refused, and the batches of the next tick are
// not.
func TestMenuBoundsRequests(t *testing.T) {
	withBreaker(t, anticheat.Default(), func(tx *world.Tx, p *Player, r *clienttest.Recorder, h *violationRecorder, tick func(int)) {
		_ = p.Inventory().SetItem(0, item.NewStack(block.Stone{}, 1))
		name := func(s menu.Slot, role menu.Role) menu.Name { return 0 }
		from := menu.Ref{Slot: menu.Slot{Inv: p.Inventory(), Index: 0}}
		to := menu.Ref{Slot: menu.Slot{Inv: p.Inventory(), Index: 1}}
		var last error
		for i := range 65 {
			b := p.BeginMenu(name)
			if i%2 == 0 {
				last = b.Move(from, to, 1)
			} else {
				last = b.Move(to, from, 1)
			}
			if i < 64 && last != nil {
				t.Fatalf("batch %d refused: %v", i, last)
			}
			if last != nil {
				b.Rollback()
			} else {
				b.Commit()
			}
		}
		if !errors.Is(last, errMenuFlood) || h.count(anticheat.Menu) != 1 {
			t.Fatalf("the 65th batch: %v, violations %+v", last, h.violations)
		}
		tick(1)
		b := p.BeginMenu(name)
		if err := b.Move(from, to, 1); err != nil {
			t.Fatalf("a batch in the next tick: %v", err)
		}
		b.Commit()
	})
}

// TestMenuOff checks that with Menu off a menu stays open far away.
func TestMenuOff(t *testing.T) {
	off := withPolicy(anticheat.Menu, func(p *anticheat.Policy) { p.Enabled = false })
	withBreaker(t, off, func(tx *world.Tx, p *Player, r *clienttest.Recorder, h *violationRecorder, tick func(int)) {
		chest := cube.Pos{1, 100, 0}
		tx.SetBlock(chest, block.NewChest(), nil)
		p.OpenBlockContainer(chest, tx)
		p.data.Pos = mgl64.Vec3{21.5, 100, 0.5}
		tick(1)
		if p.Menu() == nil {
			t.Fatal("with Menu off, the chest closed 20 blocks away")
		}
	})
}
