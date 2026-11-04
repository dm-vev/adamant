package session

import (
	"fmt"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/menu"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// levelController is a player of a game mode and a level, for the sweeps
// of the handlers of the session against the menu. It is the owner of the
// batch of the menu as well.
type levelController struct {
	Controllable
	mode  world.GameMode
	level int
}

func (c *levelController) GameMode() world.GameMode     { return c.mode }
func (c *levelController) ExperienceLevel() int         { return c.level }
func (c *levelController) SetExperienceLevel(level int) { c.level = level }
func (c *levelController) UnlockedRecipeIDs() []string  { return nil }

// stationOutcome is what a request of a menu left: whether it was
// refused, the UI slots of the menu and the level of the player.
type stationOutcome struct {
	refused bool
	slots   []string
	level   int
}

func stationSlots(ui *inventory.Inventory, slots ...int) []string {
	var s []string
	for _, slot := range slots {
		it, _ := ui.Item(slot)
		s = append(s, fmt.Sprintf("%d:%v %v", slot, it.Count(), item.WriteNBT(it)))
	}
	return s
}

// stationRun runs a request of the menu of b both ways in tx, from the same
// UI slots and a player of the game mode and level passed: through the
// handler of the session, old, and as a batch of the menu, new. Each runs
// with b placed anew, as one may change it.
func stationRun(t *testing.T, tx *world.Tx, b world.Block, ui map[int]item.Stack, mode world.GameMode, level int,
	old func(h *ItemStackRequestHandler, s *Session, c *levelController) error,
	new func(bt *menu.Tx) error, slots ...int) (stationOutcome, stationOutcome) {
	t.Helper()
	pos := cube.Pos{}
	run := func(f func(s *Session, c *levelController) error) stationOutcome {
		tx.SetBlock(pos, b, nil)
		s := craftingTestSession()
		s.ui = inventory.New(54, nil)
		for slot, it := range ui {
			_ = s.ui.SetItem(slot, it)
		}
		s.menu.Store(menu.OpenBlock(tx, pos, s, nil))
		c := &levelController{mode: mode, level: level}
		err := f(s, c)
		return stationOutcome{refused: err != nil, slots: stationSlots(s.ui, slots...), level: c.level}
	}
	o := run(func(s *Session, c *levelController) error {
		h := craftingTestHandler()
		err := old(h, s, c)
		if err != nil {
			h.reject(0, s, tx)
		}
		return err
	})
	n := run(func(s *Session, c *levelController) error {
		bt := menu.Begin(menu.Opts{
			Tx: tx, Owner: c, Menu: s.menu.Load(), Name: s.nameSlot,
			Inv: menu.Inventories{Main: s.inv, OffHand: s.offHand, UI: s.ui},
		})
		err := new(bt)
		if err != nil {
			bt.Rollback()
		} else {
			bt.Commit()
		}
		return err
	})
	return o, n
}

// TestRepairMatchesTheSession sweeps menu.Tx.Repair against the handler of
// the anvil of the session: swords of every tier, whole, damaged and
// almost broken, bare, enchanted and worked before; materials of their
// repair, other swords, enchanted books, a stick and none; a rename, none
// and one too long; in survival and creative, with too few levels and
// enough. The result, the inputs left and the levels paid must be the same,
// and so must the verdict.
func TestRepairMatchesTheSession(t *testing.T) {
	t.Parallel()
	var inputs, materials []item.Stack
	for _, tier := range item.ToolTiers() {
		sword := item.NewStack(item.Sword{Tier: tier}, 1)
		for _, damage := range []int{0, 1, sword.MaxDurability() / 3, sword.MaxDurability() - 1} {
			inputs = append(inputs, sword.Damage(damage))
		}
		inputs = append(inputs,
			sword.Damage(10).WithEnchantments(item.NewEnchantment(enchantment.Sharpness, 2), item.NewEnchantment(enchantment.Unbreaking, 3)),
			sword.WithAnvilCost(39),
			sword.WithAnvilCost(7).WithEnchantments(item.NewEnchantment(enchantment.Sharpness, 5)),
		)
		materials = append(materials, sword.Damage(sword.MaxDurability()/2), sword.WithEnchantments(item.NewEnchantment(enchantment.Sharpness, 2)))
	}
	inputs = append(inputs,
		item.NewStack(item.Sword{Tier: item.ToolTierIron}, 2).Damage(50),
		item.NewStack(item.EnchantedBook{}, 1).WithEnchantments(item.NewEnchantment(enchantment.Sharpness, 2)),
		item.NewStack(item.Stick{}, 1),
	)
	materials = append(materials,
		item.Stack{},
		item.NewStack(item.IronIngot{}, 1), item.NewStack(item.IronIngot{}, 4), item.NewStack(item.Diamond{}, 2),
		item.NewStack(block.Planks{Wood: block.OakWood()}, 3), item.NewStack(item.GoldIngot{}, 1),
		item.NewStack(item.EnchantedBook{}, 1).WithEnchantments(item.NewEnchantment(enchantment.Sharpness, 2)),
		item.NewStack(item.EnchantedBook{}, 1).WithEnchantments(item.NewEnchantment(enchantment.Sharpness, 5), item.NewEnchantment(enchantment.Looting, 3)),
		item.NewStack(item.EnchantedBook{}, 1).WithEnchantments(item.NewEnchantment(enchantment.Protection, 1)),
		item.NewStack(item.EnchantedBook{}, 1).WithEnchantments(item.NewEnchantment(enchantment.Sharpness, 2)).WithAnvilCost(15),
		item.NewStack(item.Stick{}, 1),
	)
	names := [][]string{nil, {"Blade"}, {strings.Repeat("a", 51)}}

	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		cases, accepted := 0, 0
		for _, input := range inputs {
			for _, material := range materials {
				for _, filter := range names {
					for _, mode := range []world.GameMode{world.GameModeSurvival, world.GameModeCreative} {
						for _, level := range []int{2, 39} {
							cases++
							ui := map[int]item.Stack{menu.UIAnvilInput: input, menu.UIAnvilMaterial: material}
							var name *string
							if filter != nil {
								name = &filter[0]
							}
							old, new := stationRun(t, tx, block.Anvil{Type: block.UndamagedAnvil()}, ui, mode, level,
								func(h *ItemStackRequestHandler, s *Session, c *levelController) error {
									return h.handleCraftRecipeOptional(&v2193.CraftRecipeOptionalStackRequestAction{}, s, filter, c, tx)
								},
								func(bt *menu.Tx) error { return bt.Repair(name) },
								menu.UIAnvilInput, menu.UIAnvilMaterial, menu.UIResult)
							if !old.refused {
								accepted++
							}
							if fmt.Sprint(old) != fmt.Sprint(new) {
								t.Errorf("%v on %v, rename %q, %T at level %d:\nthe session: %v\nthe menu:    %v", input, material, filter, mode, level, old, new)
							}
						}
					}
				}
			}
		}
		// The sweep would pass on two handlers that refuse everything.
		if accepted < cases/10 {
			t.Errorf("only %d of %d cases accepted", accepted, cases)
		}
	})
}
