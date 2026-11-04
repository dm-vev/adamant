package player

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/client/clienttest"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/menu"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
)

// TestPlayerShowsWhatItHolds checks the slot functions a player with a
// client installs: the client is handed the inventories; a change of the
// item held or of the off hand shows the viewers the items held, one of the
// type of the armour the armour, and the sound of it put on while a batch
// of a menu runs; and an item put in an open enchanting table shows the
// client the options of the table. Each step runs in a transaction of its
// own.
func TestPlayerShowsWhatItHolds(t *testing.T) {
	w, viewer := clientTestWorld(t)
	r := clienttest.New()
	var h *world.EntityHandle
	withClientPlayer(t, w, r, func(_ *world.Tx, p *Player) { h = p.H() })
	if calls := r.CallsTo("ViewInventories"); len(calls) == 0 {
		t.Fatal("the client was not handed the inventories")
	}
	step := func(f func(tx *world.Tx, p *Player)) {
		t.Helper()
		viewer.Reset()
		r.Reset()
		<-w.Exec(func(tx *world.Tx) {
			e, ok := h.Entity(tx)
			if !ok {
				t.Fatal("the player is not in the world")
			}
			f(tx, e.(*Player))
		})
	}
	count := func(rec *clienttest.Recorder, method string) int { return len(rec.CallsTo(method)) }
	equipSounds := func() (n int) {
		for _, c := range viewer.CallsTo("ViewSound") {
			if _, ok := c.Args[1].(sound.EquipItem); ok {
				n++
			}
		}
		return n
	}

	step(func(_ *world.Tx, p *Player) { _ = p.Inventory().SetItem(1, item.NewStack(item.Apple{}, 1)) })
	if n := count(viewer, "ViewEntityItems"); n != 0 {
		t.Errorf("a slot not held changed: the viewers were shown the items held %d times", n)
	}
	step(func(_ *world.Tx, p *Player) { _ = p.Inventory().SetItem(0, item.NewStack(item.Apple{}, 1)) })
	if n := count(viewer, "ViewEntityItems"); n != 1 {
		t.Errorf("the slot held changed: the viewers were shown the items held %d times, want once", n)
	}
	step(func(_ *world.Tx, p *Player) { _ = p.offHand.SetItem(0, item.NewStack(item.Totem{}, 1)) })
	if n := count(viewer, "ViewEntityItems"); n != 1 {
		t.Errorf("the off hand changed: the viewers were shown the items held %d times, want once", n)
	}

	helmet := item.NewStack(item.Helmet{Tier: item.ArmourTierIron{}}, 1)
	step(func(_ *world.Tx, p *Player) { p.Armour().SetHelmet(helmet) })
	if n, sounds := count(viewer, "ViewEntityArmour"), equipSounds(); n != 1 || sounds != 0 {
		t.Errorf("a helmet put on: the armour shown %d times and %d sounds played, want once and none", n, sounds)
	}
	step(func(_ *world.Tx, p *Player) { p.Armour().SetHelmet(helmet.Damage(1)) })
	if n := count(viewer, "ViewEntityArmour"); n != 0 {
		t.Errorf("the helmet damaged: the armour shown %d times, want none", n)
	}
	step(func(_ *world.Tx, p *Player) {
		b := p.BeginMenu(func(menu.Slot, menu.Role) menu.Name { return 0 })
		p.Armour().SetHelmet(item.NewStack(item.Helmet{Tier: item.ArmourTierGold{}}, 1))
		b.Commit()
	})
	if n, sounds := count(viewer, "ViewEntityArmour"), equipSounds(); n != 1 || sounds != 1 {
		t.Errorf("a helmet put on in a batch: the armour shown %d times and %d sounds played, want once each", n, sounds)
	}

	table := cube.Pos{1, 100, 0}
	step(func(tx *world.Tx, p *Player) {
		tx.SetBlock(table, block.EnchantingTable{}, nil)
		p.OpenBlockContainer(table, tx)
	})
	step(func(_ *world.Tx, p *Player) {
		_ = p.ui.SetItem(menu.UIEnchantingInput, item.NewStack(item.Pickaxe{Tier: item.ToolTierIron}, 1))
	})
	if n := count(r, "ViewEnchantmentOptions"); n != 1 {
		t.Errorf("a pickaxe put in the enchanting table: the client was shown the options %d times, want once", n)
	}
	step(func(_ *world.Tx, p *Player) { _ = p.ui.SetItem(menu.UIEnchantingInput, item.NewStack(item.Stick{}, 1)) })
	if n := count(r, "ViewEnchantmentOptions"); n != 0 {
		t.Errorf("a stick put in the enchanting table: the client was shown options %d times, want none", n)
	}
}
