package menu_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/recipe"
	"github.com/df-mc/dragonfly/server/menu"
	"github.com/df-mc/dragonfly/server/world"
)

// TestBeginAutoCraft checks what a recipe-book craft refuses to begin: a
// recipe of another kind or block, none crafted, and one not unlocked with
// limited_crafting on.
func TestBeginAutoCraft(t *testing.T) {
	inTx(t, func(tx *world.Tx) {
		o := &fakeOwner{mode: world.GameModeSurvival}
		b, _ := ownerBatch(tx, nil, o)
		for _, c := range []struct {
			name  string
			r     recipe.Recipe
			times int
		}{
			{"a special recipe", multi(repairUUID), 1},
			{"a recipe of the stonecutter", recipe.NewShapeless([]recipe.Item{stone(1)}, item.NewStack(block.StoneBricks{}, 1), "stonecutter"), 1},
			{"no times", sticksRecipe, 0},
		} {
			if _, err := b.BeginAutoCraft(c.r, c.times); err == nil {
				t.Errorf("began %s", c.name)
			}
		}
		tx.World().SetGameRule("limited_crafting", true)
		if _, err := b.BeginAutoCraft(windChargeRecipe, 1); err == nil {
			t.Error("began a recipe not unlocked with limited_crafting on")
		}
		o.unlocked = recipe.JavaRecipeIDs(windChargeRecipe)
		if _, err := b.BeginAutoCraft(windChargeRecipe, 1); err != nil {
			t.Errorf("an unlocked recipe: %v", err)
		}
	})
}

// TestAutoCraft checks the payment of a recipe-book craft of sticks: the
// two planks it takes are owed together, paid from the grid of the menu
// and the main inventory, and the slots its caller names are taken; the
// craft refuses a payment of no items, of more than a slot holds, of a
// locked item, of an item not owed and of more than is owed, and to finish
// with less paid than owed. A refused payment changes no slot.
func TestAutoCraft(t *testing.T) {
	inTx(t, func(tx *world.Tx) {
		b, inv := ownerBatch(tx, craftingMenu(tx), &fakeOwner{mode: world.GameModeSurvival})
		_ = inv.UI.SetItem(menu.UICraftingLarge+8, oakPlanks(1))
		_ = inv.Main.SetItem(0, oakPlanks(10))
		_ = inv.Main.SetItem(1, apples(3))
		_ = inv.Main.SetItem(2, oakPlanks(2).WithLockMode(item.LockInInventory))
		ac, err := b.BeginAutoCraft(sticksRecipe, 3)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range []struct {
			slot menu.Slot
			in   bool
		}{
			{menu.Slot{Inv: inv.UI, Index: menu.UICraftingLarge}, true},
			{menu.Slot{Inv: inv.UI, Index: menu.UICraftingLarge + 8}, true},
			{menu.Slot{Inv: inv.UI, Index: menu.UICraftingSmall}, false},
			{menu.Slot{Inv: inv.Main, Index: menu.UICraftingLarge}, false},
		} {
			if got := ac.InGrid(s.slot); got != s.in {
				t.Errorf("slot %d of %p in the grid: %v", s.slot.Index, s.slot.Inv, got)
			}
		}
		for _, c := range []struct {
			name  string
			r     menu.Ref
			count int
		}{
			{"no items", ref(inv.Main, 0, 0), 0},
			{"more than the slot holds", ref(inv.Main, 0, 0), 11},
			{"apples", ref(inv.Main, 1, 0), 1},
			{"locked planks", ref(inv.Main, 2, 0), 2},
		} {
			if err := ac.Pay(c.r, c.count); err == nil {
				t.Errorf("paid %s", c.name)
			}
		}
		// A payment refused may have counted part of what it paid, as a
		// failed operation leaves its batch to be rolled back: a craft of
		// its own.
		over, err := b.BeginAutoCraft(sticksRecipe, 3)
		if err != nil {
			t.Fatal(err)
		}
		if err := over.Pay(ref(inv.Main, 0, 0), 7); err == nil {
			t.Error("paid more than is owed")
		}
		if len(b.Changes()) != 0 {
			t.Fatalf("refused payments changed %v", b.Changes())
		}
		if err := ac.Pay(ref(inv.UI, menu.UICraftingLarge+8, 1), 1); err != nil {
			t.Fatal(err)
		}
		if err := ac.Pay(ref(inv.Main, 0, 2), 4); err != nil {
			t.Fatal(err)
		}
		if err := ac.Finish(); err == nil {
			t.Fatal("finished with a plank owed")
		}
		if err := ac.Pay(ref(inv.Main, 0, 2), 1); err != nil {
			t.Fatal(err)
		}
		if err := ac.Finish(); err != nil {
			t.Fatal(err)
		}
		if got := slotOf(inv.Main, 0); !got.Equal(oakPlanks(5)) {
			t.Errorf("%v left in the inventory, want five planks", got)
		}
		if got := slotOf(inv.UI, menu.UIResult); !got.Equal(item.NewStack(item.Stick{}, 12)) {
			t.Errorf("the result is %v, want twelve sticks", got)
		}
		if c := b.Changes(); len(c) != 4 || c[0].Ref.Name != 1 || c[1].Ref.Name != 2 {
			t.Errorf("the changes %v, want the slots paid from, named as their caller named them, then the result", c)
		}
	})
}
