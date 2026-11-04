package menu

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/item"
)

// Enchant takes option 0, 1 or 2 of the enchanting table of the menu: it
// costs option+1 levels and as many lapis lazuli (none in creative), needs
// the level the option shows, and resets the enchantment seed of the
// player. The options are those EnchantmentOptions shows for the input, of
// the seed of the player and the bookshelves around the table now. The
// enchanted input is the result of the craft.
func (t *Tx) Enchant(option int) error {
	if err := t.refused(); err != nil {
		return err
	}
	m := t.o.Menu
	if m == nil {
		return fmt.Errorf("no enchanting table opened")
	}
	if _, ok := t.o.Tx.Block(m.Pos()).(block.EnchantingTable); !ok {
		return fmt.Errorf("no enchanting table opened")
	}
	c := t.o.Owner

	// First ensure that the selected slot is not out of bounds.
	if option < 0 || option > 2 {
		return fmt.Errorf("invalid recipe network id: %d", option)
	}

	// Now ensure we have an input and only one input.
	input, err := t.o.Inv.UI.Item(UIEnchantingInput)
	if err != nil {
		return err
	}
	if input.Count() > 1 {
		return fmt.Errorf("enchanting tables only accept one item at a time")
	}

	// Determine the available enchantments using the player's enchantment seed.
	options, ok := EnchantmentOptions(t.o.Tx, c.EnchantmentSeed(), m.Pos(), input)
	if !ok {
		return fmt.Errorf("can't enchant non-enchantable item")
	}

	// Use the slot plus one as the cost. The requirement and enchantments can be found in the options at the same
	// slot index.
	cost := option + 1
	requirement := options[option].Cost
	enchants := options[option].Enchantments
	// EnchantmentMenu.clickMenuButton takes no option the table does not
	// offer, and changes nothing for one of no enchantments.
	if requirement <= 0 || len(enchants) == 0 {
		return fmt.Errorf("enchanting table option %d is not offered", option)
	}

	// If we don't have infinite resources, we need to deduct Lapis Lazuli and experience.
	if !c.GameMode().CreativeInventory() {
		// First ensure that the experience level is both underneath the requirement and the cost.
		if c.ExperienceLevel() < requirement {
			return fmt.Errorf("not enough levels to meet requirement")
		}
		if c.ExperienceLevel() < cost {
			return fmt.Errorf("not enough levels to meet cost")
		}

		// Then ensure that the player has input Lapis Lazuli, and enough of it to meet the cost.
		lapis, err := t.o.Inv.UI.Item(UIEnchantingLapis)
		if err != nil {
			return err
		}
		if _, ok := lapis.Item().(item.LapisLazuli); !ok {
			return fmt.Errorf("lapis lazuli was not input")
		}
		if lapis.Count() < cost {
			return fmt.Errorf("not enough lapis lazuli to meet cost")
		}

		// Deduct the experience and Lapis Lazuli.
		c.SetExperienceLevel(c.ExperienceLevel() - cost)
		_ = t.set(t.named(Slot{Inv: t.o.Inv.UI, Index: UIEnchantingLapis}, RoleEnchantingLapis), lapis.Grow(-cost))
	}

	// Reset the enchantment seed so different enchantments can be selected.
	c.ResetEnchantmentSeed()

	// Clear the existing input item, and apply the new item into the crafting result slot of the UI. The client will
	// automatically move the item into the input slot.
	_ = t.set(t.named(Slot{Inv: t.o.Inv.UI, Index: UIEnchantingInput}, RoleEnchantingInput), item.Stack{})

	return t.createResults(input.WithEnchantments(enchants...))
}
