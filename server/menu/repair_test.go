package menu_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/menu"
	"github.com/df-mc/dragonfly/server/world"
)

func name(s string) *string { return &s }

// TestRepair takes the result of an anvil for each kind of work: the
// result, the material left, the levels paid and the cost of work the
// result carries.
func TestRepair(t *testing.T) {
	ironSword := item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1)
	sharpness := item.NewStack(item.EnchantedBook{}, 1).WithEnchantments(item.NewEnchantment(enchantment.Sharpness, 2))
	for _, tc := range []struct {
		name            string
		mode            world.GameMode
		input, material item.Stack
		rename          *string
		wantLevel       int
		wantMaterial    int
		check           func(t *testing.T, res item.Stack)
	}{
		{
			name: "two ingots of three repair a sword", mode: world.GameModeSurvival,
			input: ironSword.Damage(100), material: item.NewStack(item.IronIngot{}, 3),
			wantLevel: 28, wantMaterial: 1,
			check: func(t *testing.T, res item.Stack) {
				if res.Durability() != res.MaxDurability() || res.AnvilCost() != 1 {
					t.Errorf("durability %d of %d, work %d; want whole and 1", res.Durability(), res.MaxDurability(), res.AnvilCost())
				}
			},
		},
		{
			name: "a sword repairs another", mode: world.GameModeSurvival,
			input: ironSword.Damage(200), material: ironSword.Damage(200),
			wantLevel: 28,
			check: func(t *testing.T, res item.Stack) {
				// 50 + 50 + 12% of 250.
				if res.Durability() != 130 {
					t.Errorf("durability %d, want 130", res.Durability())
				}
			},
		},
		{
			name: "a book of sharpness", mode: world.GameModeSurvival,
			input: ironSword, material: sharpness,
			wantLevel: 28,
			check: func(t *testing.T, res item.Stack) {
				if e, ok := res.Enchantment(enchantment.Sharpness); !ok || e.Level() != 2 {
					t.Errorf("enchantments %v, want sharpness II", res.Enchantments())
				}
			},
		},
		{
			// AnvilMenu checks Enchantment.canEnchant, the supported_items
			// of smite.json: #enchantable/weapon holds the mace.
			name: "a book of smite on a mace", mode: world.GameModeSurvival,
			input:     item.NewStack(item.Mace{}, 1),
			material:  item.NewStack(item.EnchantedBook{}, 1).WithEnchantments(item.NewEnchantment(enchantment.Smite, 3)),
			wantLevel: 27,
			check: func(t *testing.T, res item.Stack) {
				if e, ok := res.Enchantment(enchantment.Smite); !ok || e.Level() != 3 {
					t.Errorf("enchantments %v, want smite III", res.Enchantments())
				}
			},
		},
		{
			name: "a rename", mode: world.GameModeSurvival,
			input: ironSword, rename: name("Blade"),
			wantLevel: 29,
			check: func(t *testing.T, res item.Stack) {
				if res.CustomName() != "Blade" || res.AnvilCost() != 0 {
					t.Errorf("named %q with work %d, want Blade and 0", res.CustomName(), res.AnvilCost())
				}
			},
		},
		{
			name: "a rename of 39 levels of work costs 39", mode: world.GameModeSurvival,
			input: ironSword.WithAnvilCost(39), rename: name("Blade"),
			wantLevel: 0,
		},
		{
			name: "a rename of 50 characters", mode: world.GameModeSurvival,
			input: ironSword, rename: name(strings.Repeat("ж", item.MaxCustomNameLength)),
			wantLevel: 29,
		},
		{
			name: "a cost of 40 or more in creative", mode: world.GameModeCreative,
			input: ironSword.WithAnvilCost(39), material: sharpness,
			wantLevel: 30,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, _ := newWorld(t)
			<-w.Exec(func(tx *world.Tx) {
				level := 30
				if tc.wantLevel == 0 {
					level = 39
				}
				o := &stationOwner{mode: tc.mode, level: level}
				b, inv := stationBatch(t, tx, cube.Pos{}, block.Anvil{Type: block.UndamagedAnvil()}, o, map[int]item.Stack{
					menu.UIAnvilInput: tc.input, menu.UIAnvilMaterial: tc.material,
				})
				if err := b.Repair(tc.rename); err != nil {
					t.Fatal(err)
				}
				if o.level != tc.wantLevel {
					t.Errorf("level %d, want %d", o.level, tc.wantLevel)
				}
				if got := uiSlot(inv, menu.UIAnvilMaterial).Count(); got != tc.wantMaterial {
					t.Errorf("%d of the material left, want %d", got, tc.wantMaterial)
				}
				if !uiSlot(inv, menu.UIAnvilInput).Empty() {
					t.Error("the input is left")
				}
				res := uiSlot(inv, menu.UIResult)
				if res.Empty() {
					t.Fatal("no result")
				}
				if tc.check != nil {
					tc.check(t, res)
				}
				want := []menu.Name{
					roleName(menu.RoleAnvilInput, menu.UIAnvilInput),
					roleName(menu.RoleAnvilMaterial, menu.UIAnvilMaterial),
					roleName(menu.RoleResult, menu.UIResult),
				}
				if got := names(b); !slices.Equal(got, want) {
					t.Errorf("changes named %v, want %v", got, want)
				}
			})
		})
	}
}

// TestRepairRefused checks the requests of an anvil refused before
// anything is paid or changed.
func TestRepairRefused(t *testing.T) {
	ironSword := item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1)
	for _, tc := range []struct {
		name            string
		b               world.Block
		level           int
		input, material item.Stack
		rename          *string
	}{
		{"no anvil", block.Stone{}, 30, ironSword, item.Stack{}, name("Blade")},
		{"no input", block.Anvil{}, 30, item.Stack{}, item.NewStack(item.IronIngot{}, 1), nil},
		{"a locked input", block.Anvil{}, 30, ironSword.WithLockMode(item.LockInSlot), item.Stack{}, name("Blade")},
		{"a locked material", block.Anvil{}, 30, ironSword.Damage(10), item.NewStack(item.IronIngot{}, 1).WithLockMode(item.LockInInventory), nil},
		{"a whole sword and an ingot", block.Anvil{}, 30, ironSword, item.NewStack(item.IronIngot{}, 1), nil},
		{"a sword and a stick", block.Anvil{}, 30, ironSword.Damage(10), item.NewStack(item.Stick{}, 1), nil},
		{"no work", block.Anvil{}, 30, ironSword, item.Stack{}, nil},
		{"a book of sharpness on a mace", block.Anvil{}, 30, item.NewStack(item.Mace{}, 1), item.NewStack(item.EnchantedBook{}, 1).WithEnchantments(item.NewEnchantment(enchantment.Sharpness, 1)), nil},
		// #enchantable/armor, the supported_items of protection.json and
		// thorns.json, does not hold the elytra.
		{"a book of protection on an elytra", block.Anvil{}, 30, item.NewStack(item.Elytra{}, 1), item.NewStack(item.EnchantedBook{}, 1).WithEnchantments(item.NewEnchantment(enchantment.Protection, 1)), nil},
		{"a book of thorns on an elytra", block.Anvil{}, 30, item.NewStack(item.Elytra{}, 1), item.NewStack(item.EnchantedBook{}, 1).WithEnchantments(item.NewEnchantment(enchantment.Thorns, 1)), nil},
		{"a name of 51 characters", block.Anvil{}, 30, ironSword, item.Stack{}, name(strings.Repeat("a", item.MaxCustomNameLength+1))},
		{"a name not of UTF-8", block.Anvil{}, 30, ironSword, item.Stack{}, name("a\xffb")},
		{"levels short", block.Anvil{}, 1, ironSword.Damage(100), item.NewStack(item.IronIngot{}, 3), nil},
		{"a cost of 40 or more", block.Anvil{}, 100, ironSword.WithAnvilCost(39), item.NewStack(item.EnchantedBook{}, 1).WithEnchantments(item.NewEnchantment(enchantment.Sharpness, 2)), nil},
		{"a rename of 39 levels of work with 38", block.Anvil{}, 38, ironSword.WithAnvilCost(39), item.Stack{}, name("Blade")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, _ := newWorld(t)
			<-w.Exec(func(tx *world.Tx) {
				o := &stationOwner{mode: world.GameModeSurvival, level: tc.level}
				b, _ := stationBatch(t, tx, cube.Pos{}, tc.b, o, map[int]item.Stack{
					menu.UIAnvilInput: tc.input, menu.UIAnvilMaterial: tc.material,
				})
				if err := b.Repair(tc.rename); err == nil {
					t.Fatal("the repair was not refused")
				}
				if o.level != tc.level || len(b.Changes()) != 0 {
					t.Errorf("the refusal paid: level %d, %d changes", o.level, len(b.Changes()))
				}
				if _, ok := tc.b.(block.Anvil); ok && tx.Block(cube.Pos{}) != tc.b {
					t.Error("a refused repair wore the anvil")
				}
			})
		})
	}
	t.Run("no menu", func(t *testing.T) {
		w, _ := newWorld(t)
		<-w.Exec(func(tx *world.Tx) {
			b := menu.Begin(menu.Opts{Tx: tx, Owner: &stationOwner{}, Name: testName})
			if err := b.Repair(nil); err == nil {
				t.Fatal("a repair with no menu open was not refused")
			}
		})
	})
}

// TestRepairWearsTheAnvilLast renames items on a very damaged anvil in
// survival until it breaks, which it does at random: the anvil is still
// there when the result is put in its slot, and is gone once the repair
// ended. In creative it never wears.
func TestRepairWearsTheAnvilLast(t *testing.T) {
	w, _ := newWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		pos := cube.Pos{}
		anvil := block.Anvil{Type: block.VeryDamagedAnvil()}
		for _, mode := range []world.GameMode{world.GameModeCreative, world.GameModeSurvival} {
			broke := false
			for range 500 {
				o := &stationOwner{mode: mode, level: 30}
				b, inv := stationBatch(t, tx, pos, anvil, o, map[int]item.Stack{
					menu.UIAnvilInput: item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1),
				})
				var atResult world.Block
				inv.UI.SlotFunc(func(slot int, _, _ item.Stack) {
					if slot == menu.UIResult {
						atResult = tx.Block(pos)
					}
				})
				if err := b.Repair(name("Blade")); err != nil {
					t.Fatal(err)
				}
				if atResult != anvil {
					t.Fatalf("the block was %v when the result was made", atResult)
				}
				if _, air := tx.Block(pos).(block.Air); air {
					broke = true
					break
				}
				if tx.Block(pos) != anvil {
					t.Fatalf("a very damaged anvil became %v", tx.Block(pos))
				}
			}
			if broke != (mode == world.GameModeSurvival) {
				t.Errorf("%T: the anvil broke %v", mode, broke)
			}
		}
	})
}
