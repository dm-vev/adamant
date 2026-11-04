package menu

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/item/recipe"
)

// Cut takes the result of a stonecutter recipe times times.
//
// The stonecutter of the menu takes its input once for each craft, and the
// input must match the one input of the recipe and not be locked.
func (t *Tx) Cut(r recipe.Recipe, times int) error {
	if m := t.o.Menu; m == nil {
		return fmt.Errorf("no stonecutter container opened")
	} else if _, ok := t.o.Tx.Block(m.Pos()).(block.Stonecutter); !ok {
		return fmt.Errorf("no stonecutter container opened")
	}
	craft := r
	if _, shapeless := craft.(recipe.Shapeless); !shapeless {
		return fmt.Errorf("recipe %v is not a shapeless recipe", craft)
	}
	if craft.Block() != "stonecutter" {
		return fmt.Errorf("recipe %v is not a stonecutter recipe", craft)
	}

	timesCrafted := times
	if timesCrafted < 1 {
		return fmt.Errorf("times crafted must be at least 1")
	}

	expectedInputs := craft.Input()
	inputSlot := Slot{Inv: t.o.Inv.UI, Index: UIStonecutterInput}
	input, _ := t.Item(inputSlot)
	if err := ensureUnlockedForCrafting(input); err != nil {
		return err
	}
	if input.Count() < timesCrafted {
		return fmt.Errorf("input item count is less than number of crafts")
	}
	if !matchingStacks(input, expectedInputs[0]) {
		return fmt.Errorf("input item is not the same as expected input")
	}

	output := craft.Output()
	_ = t.set(t.named(inputSlot, RoleStonecutterInput), input.Grow(-timesCrafted))
	return t.createResults(repeatStacks(output, timesCrafted)...)
}
