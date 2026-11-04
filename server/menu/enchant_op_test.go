package menu_test

import (
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/menu"
	"github.com/df-mc/dragonfly/server/world"
)

// TestEnchant takes each option of an enchanting table for an iron pickaxe
// with the seed 2491 and no bookshelves, whose options Java offers as they
// are fixed here (and as the session offered them for the seed 7 before
// the table followed Java's draws): the option costs its index plus one in levels and
// lapis, the seed is reset, and the input goes to the result slot with the
// enchantments of the option. In creative mode nothing is paid.
func TestEnchant(t *testing.T) {
	pickaxe := item.NewStack(item.Pickaxe{Tier: item.ToolTierIron}, 1)
	options := [3][]item.Enchantment{
		{item.NewEnchantment(enchantment.Unbreaking, 1)},
		{item.NewEnchantment(enchantment.Efficiency, 1)},
		{item.NewEnchantment(enchantment.Unbreaking, 2)},
	}
	for _, tc := range []struct {
		name      string
		mode      world.GameMode
		option    int
		wantLevel int
		wantLapis int
	}{
		{"first option", world.GameModeSurvival, 0, 29, 4},
		{"second option", world.GameModeSurvival, 1, 28, 3},
		{"third option", world.GameModeSurvival, 2, 27, 2},
		{"third option in creative", world.GameModeCreative, 2, 30, 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, _ := newWorld(t)
			<-w.Exec(func(tx *world.Tx) {
				o := &stationOwner{mode: tc.mode, level: 30, seed: 2491}
				b, inv := stationBatch(t, tx, cube.Pos{}, block.EnchantingTable{}, o, map[int]item.Stack{
					menu.UIEnchantingInput: pickaxe,
					menu.UIEnchantingLapis: item.NewStack(item.LapisLazuli{}, 5),
				})
				if err := b.Enchant(tc.option); err != nil {
					t.Fatal(err)
				}
				if o.level != tc.wantLevel || o.resets != 1 {
					t.Errorf("level %d and %d seed resets, want %d and 1", o.level, o.resets, tc.wantLevel)
				}
				if got := uiSlot(inv, menu.UIEnchantingLapis).Count(); got != tc.wantLapis {
					t.Errorf("%d lapis left, want %d", got, tc.wantLapis)
				}
				if !uiSlot(inv, menu.UIEnchantingInput).Empty() {
					t.Error("the input is not cleared")
				}
				res := uiSlot(inv, menu.UIResult)
				if _, ok := res.Item().(item.Pickaxe); !ok || !slices.Equal(res.Enchantments(), options[tc.option]) {
					t.Errorf("the result is %v, want a pickaxe of %v", res, options[tc.option])
				}
				want := []menu.Name{
					roleName(menu.RoleEnchantingLapis, menu.UIEnchantingLapis),
					roleName(menu.RoleEnchantingInput, menu.UIEnchantingInput),
					roleName(menu.RoleResult, menu.UIResult),
				}
				if tc.mode == world.GameModeCreative {
					want = want[1:]
				}
				if got := names(b); !slices.Equal(got, want) {
					t.Errorf("changes named %v, want %v", got, want)
				}
			})
		})
	}
}

// TestEnchantRefused checks the requests of an enchanting table refused
// before anything is paid or changed.
func TestEnchantRefused(t *testing.T) {
	pickaxe := item.NewStack(item.Pickaxe{Tier: item.ToolTierIron}, 1)
	lapis := item.NewStack(item.LapisLazuli{}, 5)
	for _, tc := range []struct {
		name   string
		b      world.Block
		option int
		level  int
		ui     map[int]item.Stack
	}{
		{"no enchanting table", block.Stone{}, 0, 30, map[int]item.Stack{menu.UIEnchantingInput: pickaxe, menu.UIEnchantingLapis: lapis}},
		{"an option below range", block.EnchantingTable{}, -1, 30, map[int]item.Stack{menu.UIEnchantingInput: pickaxe, menu.UIEnchantingLapis: lapis}},
		{"an option above range", block.EnchantingTable{}, 3, 30, map[int]item.Stack{menu.UIEnchantingInput: pickaxe, menu.UIEnchantingLapis: lapis}},
		{"no input", block.EnchantingTable{}, 0, 30, map[int]item.Stack{menu.UIEnchantingLapis: lapis}},
		{"two books", block.EnchantingTable{}, 0, 30, map[int]item.Stack{menu.UIEnchantingInput: item.NewStack(item.Book{}, 2), menu.UIEnchantingLapis: lapis}},
		{"a stick", block.EnchantingTable{}, 0, 30, map[int]item.Stack{menu.UIEnchantingInput: item.NewStack(item.Stick{}, 1), menu.UIEnchantingLapis: lapis}},
		{"an enchanted input", block.EnchantingTable{}, 0, 30, map[int]item.Stack{menu.UIEnchantingInput: pickaxe.WithEnchantments(item.NewEnchantment(enchantment.Efficiency, 1)), menu.UIEnchantingLapis: lapis}},
		{"levels short of the requirement", block.EnchantingTable{}, 2, 6, map[int]item.Stack{menu.UIEnchantingInput: pickaxe, menu.UIEnchantingLapis: lapis}},
		{"no lapis", block.EnchantingTable{}, 0, 30, map[int]item.Stack{menu.UIEnchantingInput: pickaxe}},
		{"no lapis but stone", block.EnchantingTable{}, 0, 30, map[int]item.Stack{menu.UIEnchantingInput: pickaxe, menu.UIEnchantingLapis: item.NewStack(block.Stone{}, 5)}},
		{"lapis short of the cost", block.EnchantingTable{}, 2, 30, map[int]item.Stack{menu.UIEnchantingInput: pickaxe, menu.UIEnchantingLapis: item.NewStack(item.LapisLazuli{}, 2)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, _ := newWorld(t)
			<-w.Exec(func(tx *world.Tx) {
				o := &stationOwner{mode: world.GameModeSurvival, level: tc.level, seed: 2491}
				b, _ := stationBatch(t, tx, cube.Pos{}, tc.b, o, tc.ui)
				if err := b.Enchant(tc.option); err == nil {
					t.Fatal("the enchantment was not refused")
				}
				if o.level != tc.level || o.resets != 0 || len(b.Changes()) != 0 {
					t.Errorf("the refusal paid: level %d, %d resets, %d changes", o.level, o.resets, len(b.Changes()))
				}
			})
		})
	}
	t.Run("an option not offered", func(t *testing.T) {
		// EnchantmentMenu.clickMenuButton refuses an option of cost 0:
		// with the seed -1 the second costs 1, less than its slot+1.
		w, _ := newWorld(t)
		<-w.Exec(func(tx *world.Tx) {
			o := &stationOwner{mode: world.GameModeSurvival, level: 30, seed: -1}
			b, _ := stationBatch(t, tx, cube.Pos{}, block.EnchantingTable{}, o, map[int]item.Stack{
				menu.UIEnchantingInput: pickaxe, menu.UIEnchantingLapis: lapis,
			})
			if err := b.Enchant(1); err == nil {
				t.Fatal("the enchantment was not refused")
			}
			if o.level != 30 || o.resets != 0 || len(b.Changes()) != 0 {
				t.Errorf("the refusal paid: level %d, %d resets, %d changes", o.level, o.resets, len(b.Changes()))
			}
		})
	})
	t.Run("no menu", func(t *testing.T) {
		w, _ := newWorld(t)
		<-w.Exec(func(tx *world.Tx) {
			b := menu.Begin(menu.Opts{Tx: tx, Owner: &stationOwner{}, Name: testName})
			if err := b.Enchant(0); err == nil {
				t.Fatal("an enchantment with no menu open was not refused")
			}
		})
	})
}

// TestEnchantTwice takes an option twice in a batch: the second takes the
// result of the first again and fails after it paid, as the session did.
func TestEnchantTwice(t *testing.T) {
	w, _ := newWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		o := &stationOwner{mode: world.GameModeSurvival, level: 30, seed: 2491}
		b, inv := stationBatch(t, tx, cube.Pos{}, block.EnchantingTable{}, o, map[int]item.Stack{
			menu.UIEnchantingInput: item.NewStack(item.Pickaxe{Tier: item.ToolTierIron}, 1),
			menu.UIEnchantingLapis: item.NewStack(item.LapisLazuli{}, 5),
		})
		if err := b.Enchant(0); err != nil {
			t.Fatal(err)
		}
		_ = inv.UI.SetItem(menu.UIEnchantingInput, item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1))
		if err := b.Enchant(0); err == nil {
			t.Fatal("the result was taken twice")
		}
		if o.level != 28 || o.resets != 2 || len(b.Changes()) != 5 {
			t.Errorf("level %d, %d resets, %d changes; want 28, 2 and 5", o.level, o.resets, len(b.Changes()))
		}
		b.Rollback()
		if uiSlot(inv, menu.UIEnchantingLapis).Count() != 5 || !uiSlot(inv, menu.UIResult).Empty() {
			t.Error("the rollback did not restore the lapis and the result slot")
		}
	})
}
