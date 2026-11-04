package session

import (
	"fmt"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/menu"
	"github.com/df-mc/dragonfly/server/world"
)

// TestGrindAsTheSessionDid sweeps menu.Tx.Grind over every pair of the set
// of inputs the handler of the grindstone of the session was swept against,
// either slot empty: tools whole and damaged, bare, enchanted and cursed, of
// two kinds; books, enchanted and not; a stick, two sticks and a locked
// pickaxe. The result, the inputs left and the verdict must be those the
// session's handler gave (testdata/frozen/grind.txt). The experience the
// enchantments drop is drawn at random, and not compared.
func TestGrindAsTheSessionDid(t *testing.T) {
	t.Parallel()
	pickaxe := item.NewStack(item.Pickaxe{Tier: item.ToolTierIron}, 1)
	inputs := []item.Stack{
		{},
		pickaxe,
		pickaxe.Damage(100),
		pickaxe.Damage(240).WithEnchantments(item.NewEnchantment(enchantment.Efficiency, 5), item.NewEnchantment(enchantment.CurseOfVanishing, 1)),
		pickaxe.WithEnchantments(item.NewEnchantment(enchantment.Unbreaking, 2)).WithAnvilCost(7),
		pickaxe.WithLockMode(item.LockInSlot),
		item.NewStack(item.Sword{Tier: item.ToolTierDiamond}, 1).Damage(1000),
		item.NewStack(item.Sword{Tier: item.ToolTierDiamond}, 1).WithEnchantments(item.NewEnchantment(enchantment.Sharpness, 3)),
		item.NewStack(item.Book{}, 1),
		item.NewStack(item.EnchantedBook{}, 1).WithEnchantments(item.NewEnchantment(enchantment.Mending, 1), item.NewEnchantment(enchantment.CurseOfBinding, 1)),
		item.NewStack(item.Stick{}, 1),
		item.NewStack(item.Stick{}, 2),
	}
	frozen := &frozenSweep{name: "grind"}
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		cases, accepted := 0, 0
		for i, first := range inputs {
			for j, second := range inputs {
				cases++
				ui := map[int]item.Stack{menu.UIGrindstoneFirst: first, menu.UIGrindstoneSecond: second}
				o := stationRun(t, tx, block.Grindstone{}, ui, world.GameModeSurvival, 0,
					func(bt *menu.Tx) error { return bt.Grind() },
					menu.UIGrindstoneFirst, menu.UIGrindstoneSecond, menu.UIResult)
				if !o.refused {
					accepted++
				}
				frozen.add(fmt.Sprintf("%d %d", i, j), fmt.Sprint(o))
				for e := range tx.Entities() {
					if e.H().Type() == entity.ExperienceOrbType {
						tx.RemoveEntity(e)
					}
				}
			}
		}
		if accepted < cases/10 {
			t.Errorf("only %d of %d cases accepted", accepted, cases)
		}
	})
	frozen.check(t)
}
