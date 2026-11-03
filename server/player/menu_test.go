package player

import (
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/client/clienttest"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/menu"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// menuCalls returns the calls of r that open, close and fill menus.
func menuCalls(r *clienttest.Recorder) []clienttest.Call {
	return slices.DeleteFunc(r.Calls(), func(c clienttest.Call) bool {
		switch c.Method {
		case "OpenMenu", "CloseMenu", "ViewTrades":
			return false
		}
		return true
	})
}

// TestPlayerKeepsTheMenuItOpens checks that the player keeps the menu it
// opens and shows its client the opening and closing in order: a menu open
// for a block is not opened again, another closes it first, a lectern keeps
// no menu, and a close with no menu open shows nothing.
func TestPlayerKeepsTheMenuItOpens(t *testing.T) {
	w, _ := clientTestWorld(t)
	r := clienttest.New()
	chest, table, lectern := cube.Pos{1, 100, 0}, cube.Pos{2, 100, 0}, cube.Pos{3, 100, 0}
	withClientPlayer(t, w, r, func(tx *world.Tx, p *Player) {
		tx.SetBlock(chest, block.NewChest(), nil)
		tx.SetBlock(table, block.CraftingTable{}, nil)
		tx.SetBlock(lectern, block.Lectern{}, nil)
		r.Reset()

		p.OpenBlockContainer(chest, tx)
		chestMenu := p.Menu()
		if chestMenu == nil || chestMenu.Kind() != menu.Generic || chestMenu.Pos() != chest {
			t.Fatalf("Menu() after opening the chest = %+v", chestMenu)
		}
		if c, _ := tx.Block(chest).(block.Chest); c.Inventory(tx, chest) != chestMenu.Inventory() {
			t.Error("the menu of the chest shows another inventory than the chest's")
		}
		p.OpenBlockContainer(chest, tx)
		p.OpenBlockContainer(table, tx)
		tableMenu := p.Menu()
		if tableMenu == nil || tableMenu.Kind() != menu.Crafting {
			t.Fatalf("Menu() after opening the crafting table = %+v", tableMenu)
		}
		p.CloseMenu(true)
		if p.Menu() != nil {
			t.Fatal("a menu is still open after CloseMenu")
		}
		p.CloseMenu(false)
		p.OpenBlockContainer(lectern, tx)
		if p.Menu() != nil {
			t.Fatal("the player keeps a menu open for a lectern")
		}

		calls := menuCalls(r)
		if got, want := methods(calls), []string{"OpenMenu", "CloseMenu", "OpenMenu", "CloseMenu", "OpenMenu"}; !slices.Equal(got, want) {
			t.Fatalf("calls = %v, want %v", got, want)
		}
		if calls[0].Args[0] != chestMenu || calls[1].Args[0] != chestMenu || calls[1].Args[1] != false {
			t.Errorf("the chest was not opened, then closed by the server: %v %v", calls[0].Args, calls[1].Args)
		}
		if calls[2].Args[0] != tableMenu || calls[3].Args[0] != tableMenu || calls[3].Args[1] != true {
			t.Errorf("the crafting table was not opened, then closed by the client: %v %v", calls[2].Args, calls[3].Args)
		}
		if m := calls[4].Args[0].(*menu.Menu); m.Kind() != menu.Lectern || m.Pos() != lectern {
			t.Errorf("the lectern opened as %+v", m)
		}
	})
}

// TestPlayerWithoutClientOpensNoMenu checks that a player without a client
// opens no menu.
func TestPlayerWithoutClientOpensNoMenu(t *testing.T) {
	w, _ := clientTestWorld(t)
	withClientPlayer(t, w, nil, func(tx *world.Tx, p *Player) {
		pos := cube.Pos{1, 100, 0}
		tx.SetBlock(pos, block.NewChest(), nil)
		p.OpenBlockContainer(pos, tx)
		if p.Menu() != nil {
			t.Fatal("a player without a client opened a menu")
		}
	})
}

// TestPlayerOpensTheTradesOfAMerchant checks that the player shows its
// client the trades of a villager after the menu opens, and that the
// villager trades with the player until the menu closes.
func TestPlayerOpensTheTradesOfAMerchant(t *testing.T) {
	w, _ := clientTestWorld(t)
	r := clienttest.New()
	withClientPlayer(t, w, r, func(tx *world.Tx, p *Player) {
		h, err := entity.NewFromStructureNBT(map[string]any{"id": "minecraft:villager",
			"VillagerData": map[string]any{"type": "minecraft:plains", "profession": "minecraft:farmer", "level": int32(1)}},
			world.EntitySpawnOpts{Position: mgl64.Vec3{1.5, 100, 0.5}, SpawnFinalized: true})
		if err != nil {
			t.Fatal(err)
		}
		v := tx.AddEntity(h).(*entity.Villager)
		r.Reset()

		p.OpenMerchant(v, v, tx)
		m := p.Menu()
		if m == nil || m.Kind() != menu.Merchant || m.Entity() != v.H() {
			t.Fatalf("Menu() after opening the villager = %+v", m)
		}
		if v.StartTrading(entity.NewItem(world.EntitySpawnOpts{}, item.NewStack(item.Emerald{}, 1))) {
			t.Error("the villager trades with another player while the menu is open")
		}
		p.OpenMerchant(v, v, tx)
		p.CloseMenu(false)
		if !v.StartTrading(p.H()) {
			t.Error("the villager still trades with the player after the menu closed")
		}

		calls := menuCalls(r)
		if got, want := methods(calls), []string{"OpenMenu", "ViewTrades", "CloseMenu"}; !slices.Equal(got, want) {
			t.Fatalf("calls = %v, want %v", got, want)
		}
		offers, _ := v.MerchantOffersFor(p)
		if got := calls[1].Args[2].([]entity.MerchantOffer); calls[1].Args[0] != m || len(got) != len(offers) {
			t.Errorf("ViewTrades(%v, %d offers), want the menu and %d offers", calls[1].Args[0], len(got), len(offers))
		}
	})
}

// TestPlayerBeginsBatchesOfItsMenu checks that a batch of BeginMenu works
// on the inventories of the player and the menu it has open, and that the
// player knows a batch runs until it ends.
func TestPlayerBeginsBatchesOfItsMenu(t *testing.T) {
	w, _ := clientTestWorld(t)
	r := clienttest.New()
	withClientPlayer(t, w, r, func(tx *world.Tx, p *Player) {
		chest := cube.Pos{1, 100, 0}
		tx.SetBlock(chest, block.NewChest(), nil)
		p.OpenBlockContainer(chest, tx)
		m := p.Menu()
		_ = p.Inventory().SetItem(0, item.NewStack(item.Apple{}, 3))

		names := func(s menu.Slot, role menu.Role) menu.Name { return menu.Name(role) }
		b := p.BeginMenu(names)
		if !p.menuTx.Load() {
			t.Fatal("no batch runs after BeginMenu")
		}
		if err := b.SetSlot(menu.Ref{Slot: menu.Slot{Inv: m.Inventory(), Index: 4}}, item.NewStack(item.Apple{}, 1)); err != nil {
			t.Fatal(err)
		}
		if err := b.SetSlot(menu.Ref{Slot: menu.Slot{Inv: p.Inventory(), Index: 0}}, item.NewStack(item.Apple{}, 2)); err != nil {
			t.Fatal(err)
		}
		b.Rollback()
		if p.menuTx.Load() {
			t.Error("a batch still runs after its rollback")
		}
		if got, _ := m.Inventory().Item(4); !got.Empty() {
			t.Errorf("the chest holds %v after the rollback", got)
		}
		if got, _ := p.Inventory().Item(0); got.Count() != 3 {
			t.Errorf("the inventory holds %v after the rollback, want 3 apples", got)
		}
	})
}
