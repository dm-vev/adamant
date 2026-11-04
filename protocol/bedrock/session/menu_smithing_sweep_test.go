package session

import (
	"fmt"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/recipe"
	"github.com/df-mc/dragonfly/server/menu"
	"github.com/df-mc/dragonfly/server/world"
)

// recovered runs f and returns its error, or one of the panic it raised.
func recovered(f func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return f()
}

// TestSmithAsTheSessionDid sweeps menu.Tx.Smith over what the handler of
// the smithing table of the session was swept against: smithing transforms
// and trims as the game registers them, and a recipe of another block, over
// inputs, materials and templates that match and that do not, a locked
// input and empty slots. The result, the inputs left and the verdict must
// be those the session's handler gave (testdata/frozen/smith.txt); a slot
// left empty that a recipe names panics in both (matchingStacks).
func TestSmithAsTheSessionDid(t *testing.T) {
	t.Parallel()
	// The recipes as recipe.registerVanilla registers them, which only a
	// server does: transforms of stacks, the base of one of variants, and
	// trims of the tags of armour and materials, one for each template.
	upgrade := item.NewStack(item.SmithingTemplate{Template: item.TemplateNetheriteUpgrade()}, 1)
	ingot := item.NewStack(item.NetheriteIngot{}, 1)
	recipes := []recipe.Recipe{
		recipe.NewSmithingTransform(item.NewStack(item.Sword{Tier: item.ToolTierDiamond}, 1), ingot, upgrade,
			item.NewStack(item.Sword{Tier: item.ToolTierNetherite}, 1), "smithing_table"),
		recipe.NewSmithingTransform(item.NewStack(item.Chestplate{Tier: item.ArmourTierDiamond{}}, 1).WithValue("variants", true), ingot, upgrade,
			item.NewStack(item.Chestplate{Tier: item.ArmourTierNetherite{}}, 1), "smithing_table"),
		recipe.NewSmithingTrim(recipe.NewItemTag("minecraft:trimmable_armors", 1), recipe.NewItemTag("minecraft:trim_materials", 1),
			item.NewStack(item.SmithingTemplate{Template: item.TemplateCoast()}, 1), "smithing_table"),
		recipe.NewSmithingTrim(recipe.NewItemTag("minecraft:trimmable_armors", 1), recipe.NewItemTag("minecraft:trim_materials", 1),
			item.NewStack(item.SmithingTemplate{Template: item.TemplateSentry()}, 1), "smithing_table"),
		recipe.NewShapeless([]recipe.Item{item.NewStack(item.Stick{}, 1)}, item.NewStack(item.Stick{}, 1), "crafting_table"),
	}
	sword := item.NewStack(item.Sword{Tier: item.ToolTierDiamond}, 1)
	inputs := []item.Stack{
		{},
		sword.Damage(40).WithCustomName("Kept"),
		sword.WithLockMode(item.LockInInventory),
		item.NewStack(item.Chestplate{Tier: item.ArmourTierDiamond{}}, 1),
		item.NewStack(item.Helmet{Tier: item.ArmourTierIron{}}, 1),
		item.NewStack(item.Chestplate{Tier: item.ArmourTierNetherite{}}, 1),
		item.NewStack(item.Stick{}, 1),
	}
	materials := []item.Stack{
		{},
		item.NewStack(item.NetheriteIngot{}, 2),
		item.NewStack(item.IronIngot{}, 1),
		item.NewStack(item.AmethystShard{}, 1),
		item.NewStack(item.Stick{}, 1),
	}
	templates := []item.Stack{
		{},
		item.NewStack(item.SmithingTemplate{Template: item.TemplateNetheriteUpgrade()}, 1),
		item.NewStack(item.SmithingTemplate{Template: item.TemplateCoast()}, 2),
		item.NewStack(item.SmithingTemplate{Template: item.TemplateSentry()}, 1),
		item.NewStack(item.Stick{}, 1),
	}
	frozen := &frozenSweep{name: "smith", fields: 2}
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		cases, accepted := 0, 0
		for ri, r := range recipes {
			for i, input := range inputs {
				for j, material := range materials {
					for k, template := range templates {
						cases++
						ui := map[int]item.Stack{menu.UISmithingInput: input, menu.UISmithingMaterial: material, menu.UISmithingTemplate: template}
						o := stationRun(t, tx, block.SmithingTable{}, ui, world.GameModeSurvival, 0,
							func(bt *menu.Tx) error { return recovered(func() error { return bt.Smith(r) }) },
							menu.UISmithingInput, menu.UISmithingMaterial, menu.UISmithingTemplate, menu.UIResult)
						if !o.refused {
							accepted++
						}
						frozen.add(fmt.Sprintf("%d %d %d %d", ri, i, j, k), fmt.Sprint(o))
					}
				}
			}
		}
		t.Logf("%d of %d accepted", accepted, cases)
		if accepted < 10 {
			t.Errorf("only %d of %d cases accepted", accepted, cases)
		}
	})
	frozen.check(t)
}
