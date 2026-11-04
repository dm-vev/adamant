package menu_test

import (
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/recipe"
	"github.com/df-mc/dragonfly/server/menu"
	"github.com/df-mc/dragonfly/server/world"
)

var (
	upgradeTemplate = item.NewStack(item.SmithingTemplate{Template: item.TemplateNetheriteUpgrade()}, 1)
	coastTemplate   = item.NewStack(item.SmithingTemplate{Template: item.TemplateCoast()}, 1)
	netheriteSword  = recipe.NewSmithingTransform(item.NewStack(item.Sword{Tier: item.ToolTierDiamond}, 1),
		item.NewStack(item.NetheriteIngot{}, 1), upgradeTemplate, item.NewStack(item.Sword{Tier: item.ToolTierNetherite}, 1), "smithing_table")
	coastTrim = recipe.NewSmithingTrim(recipe.NewItemTag("minecraft:trimmable_armors", 1),
		recipe.NewItemTag("minecraft:trim_materials", 1), coastTemplate, "smithing_table")
)

// TestSmith upgrades a diamond sword to netherite and trims a chestplate
// with the coast trim of iron: one of each input is taken, and the result
// is one item.
func TestSmith(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		r                         recipe.Recipe
		input, material, template item.Stack
		check                     func(t *testing.T, res item.Stack)
	}{
		{
			name: "a netherite upgrade", r: netheriteSword,
			input:    item.NewStack(item.Sword{Tier: item.ToolTierDiamond}, 1),
			material: item.NewStack(item.NetheriteIngot{}, 3), template: upgradeTemplate.Grow(1),
			check: func(t *testing.T, res item.Stack) {
				if s, ok := res.Item().(item.Sword); !ok || s.Tier != item.ToolTierNetherite {
					t.Errorf("the result is %v, want a netherite sword", res)
				}
			},
		},
		{
			name: "a coast trim of iron", r: coastTrim,
			input:    item.NewStack(item.Chestplate{Tier: item.ArmourTierDiamond{}}, 1),
			material: item.NewStack(item.IronIngot{}, 3), template: coastTemplate.Grow(1),
			check: func(t *testing.T, res item.Stack) {
				c, ok := res.Item().(item.Chestplate)
				if !ok || c.Trim.Template != item.TemplateCoast() || c.Trim.Material != (item.IronIngot{}) {
					t.Errorf("the result is %v, want a chestplate of the coast trim of iron", res)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, _ := newWorld(t)
			<-w.Exec(func(tx *world.Tx) {
				b, inv := stationBatch(t, tx, cube.Pos{}, block.SmithingTable{}, &stationOwner{}, map[int]item.Stack{
					menu.UISmithingInput: tc.input, menu.UISmithingMaterial: tc.material, menu.UISmithingTemplate: tc.template,
				})
				if err := b.Smith(tc.r); err != nil {
					t.Fatal(err)
				}
				if uiSlot(inv, menu.UISmithingInput).Count() != 0 || uiSlot(inv, menu.UISmithingMaterial).Count() != 2 || uiSlot(inv, menu.UISmithingTemplate).Count() != 1 {
					t.Error("not one of each input was taken")
				}
				res := uiSlot(inv, menu.UIResult)
				if res.Count() != 1 {
					t.Fatalf("the result is %v, want one item", res)
				}
				tc.check(t, res)
				want := []menu.Name{
					roleName(menu.RoleSmithingInput, menu.UISmithingInput),
					roleName(menu.RoleSmithingMaterial, menu.UISmithingMaterial),
					roleName(menu.RoleSmithingTemplate, menu.UISmithingTemplate),
					roleName(menu.RoleResult, menu.UIResult),
				}
				if got := names(b); !slices.Equal(got, want) {
					t.Errorf("changes named %v, want %v", got, want)
				}
			})
		})
	}
}

// TestSmithRefused checks the requests of a smithing table refused before
// anything changes.
func TestSmithRefused(t *testing.T) {
	sword := item.NewStack(item.Sword{Tier: item.ToolTierDiamond}, 1)
	ingot := item.NewStack(item.NetheriteIngot{}, 1)
	for _, tc := range []struct {
		name                      string
		b                         world.Block
		r                         recipe.Recipe
		input, material, template item.Stack
	}{
		{"no smithing table", block.Stone{}, netheriteSword, sword, ingot, upgradeTemplate},
		{"a recipe of the crafting table", block.SmithingTable{}, recipe.NewShapeless([]recipe.Item{sword}, sword, "crafting_table"), sword, ingot, upgradeTemplate},
		{"a shapeless recipe of the smithing table", block.SmithingTable{}, recipe.NewShapeless([]recipe.Item{sword}, sword, "smithing_table"), sword, ingot, upgradeTemplate},
		{"an iron sword", block.SmithingTable{}, netheriteSword, item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1), ingot, upgradeTemplate},
		{"a locked sword", block.SmithingTable{}, netheriteSword, sword.WithLockMode(item.LockInSlot), ingot, upgradeTemplate},
		{"an iron ingot", block.SmithingTable{}, netheriteSword, sword, item.NewStack(item.IronIngot{}, 1), upgradeTemplate},
		{"a locked ingot", block.SmithingTable{}, netheriteSword, sword, ingot.WithLockMode(item.LockInInventory), upgradeTemplate},
		{"the coast template", block.SmithingTable{}, netheriteSword, sword, ingot, coastTemplate},
		{"a locked template", block.SmithingTable{}, netheriteSword, sword, ingot, upgradeTemplate.WithLockMode(item.LockInSlot)},
		{"a sword trimmed", block.SmithingTable{}, coastTrim, sword, item.NewStack(item.IronIngot{}, 1), coastTemplate},
		{"a trim of a stick", block.SmithingTable{}, coastTrim, item.NewStack(item.Chestplate{Tier: item.ArmourTierIron{}}, 1), item.NewStack(item.Stick{}, 1), coastTemplate},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, _ := newWorld(t)
			<-w.Exec(func(tx *world.Tx) {
				b, _ := stationBatch(t, tx, cube.Pos{}, tc.b, &stationOwner{}, map[int]item.Stack{
					menu.UISmithingInput: tc.input, menu.UISmithingMaterial: tc.material, menu.UISmithingTemplate: tc.template,
				})
				if err := b.Smith(tc.r); err == nil {
					t.Fatal("the recipe was not refused")
				}
				if len(b.Changes()) != 0 {
					t.Errorf("the refusal changed %d slots", len(b.Changes()))
				}
			})
		})
	}
	t.Run("no menu", func(t *testing.T) {
		w, _ := newWorld(t)
		<-w.Exec(func(tx *world.Tx) {
			b := menu.Begin(menu.Opts{Tx: tx, Owner: &stationOwner{}, Name: testName})
			if err := b.Smith(netheriteSword); err == nil {
				t.Fatal("a recipe with no menu open was not refused")
			}
		})
	})
}
