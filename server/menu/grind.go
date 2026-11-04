package menu

import (
	"fmt"
	"math"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
)

// Grind takes the result of the grindstone of the menu: one input, or two
// of a durable kind merged with 5% of their durability to spare, stripped
// of all enchantments but curses. The enchantments removed drop experience
// at the eyes of the player, between half their cost and all of it.
func (t *Tx) Grind() error {
	if err := t.refused(); err != nil {
		return err
	}
	// First check if there actually is a grindstone opened.
	m := t.o.Menu
	if m == nil {
		return fmt.Errorf("no grindstone container opened")
	}
	if _, ok := t.o.Tx.Block(m.Pos()).(block.Grindstone); !ok {
		return fmt.Errorf("no grindstone container opened")
	}

	// Next, get both input items and ensure they are comparable.
	firstInput, _ := t.Item(Slot{Inv: t.o.Inv.UI, Index: UIGrindstoneFirst})
	if !firstInput.Empty() {
		if err := ensureUnlockedForCrafting(firstInput); err != nil {
			return err
		}
	}
	secondInput, _ := t.Item(Slot{Inv: t.o.Inv.UI, Index: UIGrindstoneSecond})
	if !secondInput.Empty() {
		if err := ensureUnlockedForCrafting(secondInput); err != nil {
			return err
		}
	}
	if firstInput.Empty() && secondInput.Empty() {
		return fmt.Errorf("input item(s) are empty")
	}
	if firstInput.Count() > 1 || secondInput.Count() > 1 {
		return fmt.Errorf("input item(s) are not single items")
	}

	resultStack := nonZeroItem(firstInput, secondInput)
	if !firstInput.Empty() && !secondInput.Empty() {
		name, meta := firstInput.Item().EncodeItem()
		name2, meta2 := secondInput.Item().EncodeItem()
		if name != name2 || meta != meta2 {
			return fmt.Errorf("input items must be the same type")
		}
		if _, ok := firstInput.Item().(item.Durable); !ok {
			return fmt.Errorf("input items must be durable")
		}

		// We add the enchantments to the result stack in order to calculate the gained experience. These enchantments
		// are stripped when creating the result.
		resultStack = firstInput.WithEnchantments(secondInput.Enchantments()...)

		// Merge the durability of the two input items at 5%.
		maxDurability := firstInput.MaxDurability()
		firstDurability, secondDurability := firstInput.Durability(), secondInput.Durability()

		resultStack = resultStack.WithDurability(firstDurability + secondDurability + maxDurability*5/100)
	}

	for _, o := range entity.NewExperienceOrbs(entity.EyePosition(t.o.Owner), experienceFromEnchantments(resultStack)) {
		t.o.Tx.AddEntity(o)
	}

	_ = t.set(t.named(Slot{Inv: t.o.Inv.UI, Index: UIGrindstoneFirst}, RoleGrindstoneFirst), item.Stack{})
	_ = t.set(t.named(Slot{Inv: t.o.Inv.UI, Index: UIGrindstoneSecond}, RoleGrindstoneSecond), item.Stack{})
	return t.createResults(stripPossibleEnchantments(resultStack))
}

// curseEnchantment represents an enchantment that may be a curse enchantment.
type curseEnchantment interface {
	Curse() bool
}

// experienceFromEnchantments returns the amount of experience that is gained from the enchantments on the given stack.
func experienceFromEnchantments(stack item.Stack) int {
	var totalCost int
	for _, enchant := range stack.Enchantments() {
		if _, ok := enchant.Type().(curseEnchantment); ok {
			continue
		}
		cost, _ := enchant.Type().Cost(enchant.Level())
		totalCost += cost
	}
	if totalCost == 0 {
		// No cost, no experience.
		return 0
	}

	minExperience := int(math.Ceil(float64(totalCost) / 2))
	maxExperience := totalCost
	rangeSize := maxExperience - minExperience + 1
	if rangeSize <= 0 {
		// Guard against unexpected cost math to avoid panics.
		return minExperience
	}
	// Minecraft returns a value between ceil(total/2) and total, inclusive.
	return minExperience + rand.IntN(rangeSize)
}

// stripPossibleEnchantments strips all enchantments possible, excluding curses.
func stripPossibleEnchantments(stack item.Stack) item.Stack {
	for _, enchant := range stack.Enchantments() {
		if _, ok := enchant.Type().(curseEnchantment); ok {
			continue
		}
		stack = stack.WithoutEnchantments(enchant.Type())
	}
	return stack.WithAnvilCost(0)
}

// nonZeroItem returns the item.Stack that exists out of two input items. The function expects at least one of the
// items to be non-empty.
func nonZeroItem(first, second item.Stack) item.Stack {
	if first.Empty() {
		return second
	}
	return first
}
