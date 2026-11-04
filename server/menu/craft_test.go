package menu_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/recipe"
	"github.com/df-mc/dragonfly/server/menu"
	"github.com/df-mc/dragonfly/server/world"
)

var (
	oakPlanks = func(n int) item.Stack { return item.NewStack(block.Planks{Wood: block.OakWood()}, n) }
	// sticksRecipe is the recipe of four sticks of two planks.
	sticksRecipe = recipe.NewShaped([]recipe.Item{recipe.NewItemTag("minecraft:planks", 1), recipe.NewItemTag("minecraft:planks", 1)},
		item.NewStack(item.Stick{}, 4), recipe.Shape{1, 2}, "crafting_table")
	// windChargeRecipe is the recipe of four wind charges of a breeze rod.
	windChargeRecipe = recipe.NewShapeless([]recipe.Item{item.NewStack(item.BreezeRod{}, 1)}, item.NewStack(item.WindCharge{}, 4), "crafting_table")
)

// craftingMenu opens the menu of a crafting table in tx.
func craftingMenu(tx *world.Tx) *menu.Menu {
	pos := cube.Pos{0, 64, 0}
	tx.SetBlock(pos, block.CraftingTable{}, nil)
	return menu.OpenBlock(tx, pos, &fakeViewer{}, nil)
}

// TestCraft checks crafts of the crafting table in the grid of the
// inventory and of a crafting table: the inputs taken from the grid, as
// many as the times crafted, and the result in the result slot, split into
// results the client takes one by one past the size of a stack; and the
// refusals of a grid that lacks an input or holds more, of no times, and of
// a recipe of another block or kind.
func TestCraft(t *testing.T) {
	for _, c := range []struct {
		name    string
		table   bool
		r       recipe.Recipe
		times   int
		grid    map[int]item.Stack
		ok      bool
		left    map[int]item.Stack
		results []item.Stack
	}{
		{name: "sticks in the inventory", r: sticksRecipe, times: 1, grid: map[int]item.Stack{0: oakPlanks(1), 2: oakPlanks(3)},
			ok: true, left: map[int]item.Stack{2: oakPlanks(2)}, results: []item.Stack{item.NewStack(item.Stick{}, 4)}},
		{name: "sticks in the table, three times", table: true, r: sticksRecipe, times: 3, grid: map[int]item.Stack{4: oakPlanks(3), 7: oakPlanks(5)},
			ok: true, left: map[int]item.Stack{7: oakPlanks(2)}, results: []item.Stack{item.NewStack(item.Stick{}, 12)}},
		{name: "wind charges seventeen times: two results", table: true, r: windChargeRecipe, times: 17, grid: map[int]item.Stack{8: item.NewStack(item.BreezeRod{}, 17)},
			ok: true, results: []item.Stack{item.NewStack(item.WindCharge{}, 64), item.NewStack(item.WindCharge{}, 4)}},
		{name: "sticks of the grid of the table out of the inventory", r: sticksRecipe, times: 1, grid: map[int]item.Stack{4: oakPlanks(1), 5: oakPlanks(1)}},
		{name: "sticks of one plank", r: sticksRecipe, times: 1, grid: map[int]item.Stack{0: oakPlanks(1)}},
		{name: "sticks twice of a plank each", r: sticksRecipe, times: 2, grid: map[int]item.Stack{0: oakPlanks(1), 1: oakPlanks(1)}},
		{name: "sticks with an apple", r: sticksRecipe, times: 1, grid: map[int]item.Stack{0: oakPlanks(1), 1: oakPlanks(1), 3: apples(1)}},
		{name: "sticks no times", r: sticksRecipe, grid: map[int]item.Stack{0: oakPlanks(1), 1: oakPlanks(1)}},
		{name: "sticks of a locked plank", r: sticksRecipe, times: 1, grid: map[int]item.Stack{0: oakPlanks(1).WithLockMode(item.LockInInventory), 1: oakPlanks(1)}},
		{name: "a recipe of the stonecutter", r: recipe.NewShapeless([]recipe.Item{stone(1)}, item.NewStack(block.StoneBricks{}, 1), "stonecutter"), times: 1, grid: map[int]item.Stack{0: stone(1)}},
		{name: "a special recipe", r: recipe.NewMulti([16]byte{1}), times: 1, grid: map[int]item.Stack{0: stone(1)}},
	} {
		t.Run(c.name, func(t *testing.T) {
			inTx(t, func(tx *world.Tx) {
				var m *menu.Menu
				if c.table {
					m = craftingMenu(tx)
				}
				b, inv := ownerBatch(tx, m, &fakeOwner{mode: world.GameModeSurvival})
				first, size := m.Grid()
				for slot, it := range c.grid {
					_ = inv.UI.SetItem(first+slot, it)
				}
				err := b.Craft(c.r, c.times)
				if (err == nil) != c.ok {
					t.Fatalf("Craft: %v, want accepted %v", err, c.ok)
				}
				if !c.ok {
					if len(b.Changes()) != 0 {
						t.Errorf("a refused craft changed %v", b.Changes())
					}
					return
				}
				for slot := range size {
					if got := slotOf(inv.UI, first+slot); !got.Equal(c.left[slot]) {
						t.Errorf("grid slot %d holds %v, want %v", slot, got, c.left[slot])
					}
				}
				if len(c.results) > 1 {
					if !slotOf(inv.UI, menu.UIResult).Empty() {
						t.Fatal("a result of several was taken before the client took it")
					}
					for i := range c.results {
						if err := b.TakeResult(i); err != nil {
							t.Fatal(err)
						}
						if got := slotOf(inv.UI, menu.UIResult); !got.Equal(c.results[i]) {
							t.Errorf("result %d is %v, want %v", i, got, c.results[i])
						}
					}
				} else if got := slotOf(inv.UI, menu.UIResult); !got.Equal(c.results[0]) {
					t.Errorf("the result slot holds %v, want %v", got, c.results[0])
				}
				for _, ch := range b.Changes() {
					if ch.Ref.Index != menu.UIResult && ch.Ref.Name != testName(ch.Ref.Slot, menu.RoleCraftingGrid) {
						t.Errorf("slot %d of the grid named %v", ch.Ref.Index, ch.Ref.Name)
					}
				}
			})
		})
	}
}

// TestCraftKeepsUserData checks a user-data shapeless recipe: a shulker
// box dyed keeps what it holds and its name, whatever the data of the
// recipe's input.
func TestCraftKeepsUserData(t *testing.T) {
	inTx(t, func(tx *world.Tx) {
		b, inv := ownerBatch(tx, nil, &fakeOwner{mode: world.GameModeSurvival})
		box := func(t block.ShulkerBoxType) block.ShulkerBox {
			b := block.NewShulkerBox()
			b.Type = t
			return b
		}
		red := box(block.RedShulkerBox())
		_ = red.Inventory(nil, cube.Pos{}).SetItem(0, apples(3))
		dye := item.NewStack(item.Dye{Colour: item.ColourBlue()}, 1)
		_ = inv.UI.SetItem(menu.UICraftingSmall, item.NewStack(red, 1).WithCustomName("Supplies"))
		_ = inv.UI.SetItem(menu.UICraftingSmall+1, dye)
		r := recipe.NewUserDataShapeless([]recipe.Item{item.NewStack(box(block.RedShulkerBox()), 1), dye}, item.NewStack(box(block.BlueShulkerBox()), 1), "crafting_table")
		if err := b.Craft(r, 1); err != nil {
			t.Fatal(err)
		}
		result := slotOf(inv.UI, menu.UIResult)
		blue, ok := result.Item().(block.ShulkerBox)
		if !ok || blue.Type != block.BlueShulkerBox() {
			t.Fatalf("the result is %v, want a blue shulker box", result)
		}
		if held, _ := blue.Inventory(nil, cube.Pos{}).Item(0); !held.Equal(apples(3)) || result.CustomName() != "Supplies" {
			t.Errorf("the box holds %v and is named %q, want three apples and Supplies", held, result.CustomName())
		}
		if err := b.Craft(r, 1); err == nil {
			t.Error("the box was dyed again of an empty grid")
		}
	})
}

// TestCraftLimited checks limited_crafting: a recipe the player has not
// unlocked is refused while it is on.
func TestCraftLimited(t *testing.T) {
	inTx(t, func(tx *world.Tx) {
		o := &fakeOwner{mode: world.GameModeSurvival}
		// One batch for each craft: of single results, a batch takes the
		// first only.
		craft := func() error {
			b, inv := ownerBatch(tx, nil, o)
			_ = inv.UI.SetItem(menu.UICraftingSmall, item.NewStack(item.BreezeRod{}, 1))
			return b.Craft(windChargeRecipe, 1)
		}
		tx.World().SetGameRule("limited_crafting", true)
		if err := craft(); err == nil {
			t.Fatal("a recipe not unlocked was crafted with limited_crafting on")
		}
		o.unlocked = recipe.JavaRecipeIDs(windChargeRecipe)
		if err := craft(); err != nil {
			t.Fatalf("an unlocked recipe: %v", err)
		}
		tx.World().SetGameRule("limited_crafting", false)
		o.unlocked = nil
		if err := craft(); err != nil {
			t.Fatalf("with limited_crafting off: %v", err)
		}
	})
}

// TestCraftDynamic checks a dynamic recipe, wolf armour dyed: crafted no
// more often than the smallest stack of the grid allows, every stack of the
// grid consumed, and refused for no times and a grid it does not match.
func TestCraftDynamic(t *testing.T) {
	inTx(t, func(tx *world.Tx) {
		b, inv := ownerBatch(tx, craftingMenu(tx), &fakeOwner{mode: world.GameModeSurvival})
		if err := b.CraftDynamic(1); err == nil {
			t.Error("a dynamic recipe of an empty grid")
		}
		_ = inv.UI.SetItem(menu.UICraftingLarge, item.NewStack(item.WolfArmor{}, 1))
		_ = inv.UI.SetItem(menu.UICraftingLarge+4, item.NewStack(item.Dye{Colour: item.ColourLime()}, 3))
		if err := b.CraftDynamic(0); err == nil {
			t.Error("a dynamic recipe crafted no times")
		}
		if err := b.CraftDynamic(3); err != nil {
			t.Fatal(err)
		}
		if got := slotOf(inv.UI, menu.UICraftingLarge+4); got.Count() != 2 {
			t.Errorf("%v dye left of three, the armour dyed once", got)
		}
		if got := slotOf(inv.UI, menu.UIResult); got.Count() != 1 {
			t.Errorf("the result slot holds %v, want one dyed wolf armour", got)
		}
	})
}
