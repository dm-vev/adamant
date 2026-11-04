package menu

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/recipe"
)

// Smith takes the result of a smithing transform or trim recipe at the
// smithing table of the menu: the input, the material and the template
// must match the inputs of the recipe and are taken one each. A transform
// makes the input the output of the recipe, keeping what the input holds;
// a trim puts the trim of the template and the material on the input.
// An empty slot fails it with a MissingInputError: of the input or the
// material, the first empty, before the template, as BDS 1.26.52.3 tells
// them.
func (t *Tx) Smith(craft recipe.Recipe) error {
	if err := t.refused(); err != nil {
		return err
	}
	m := t.o.Menu
	if m == nil {
		return fmt.Errorf("no smithing table opened")
	}
	if _, ok := t.o.Tx.Block(m.Pos()).(block.SmithingTable); !ok {
		return fmt.Errorf("no smithing table opened")
	}

	// First, check the recipe and ensure it is valid for the smithing table.
	if craft.Block() != "smithing_table" {
		return fmt.Errorf("recipe is not a smithing table recipe")
	}
	switch craft.(type) {
	case recipe.SmithingTransform, recipe.SmithingTrim:
	default:
		return fmt.Errorf("recipe is not a smithing recipe")
	}

	input, _ := t.Item(Slot{Inv: t.o.Inv.UI, Index: UISmithingInput})
	material, _ := t.Item(Slot{Inv: t.o.Inv.UI, Index: UISmithingMaterial})
	template, _ := t.Item(Slot{Inv: t.o.Inv.UI, Index: UISmithingTemplate})
	for _, in := range [...]struct {
		stack item.Stack
		role  Role
	}{{input, RoleSmithingInput}, {material, RoleSmithingMaterial}, {template, RoleSmithingTemplate}} {
		if in.stack.Empty() {
			return MissingInputError{Role: in.role}
		}
	}

	// Check if the input item and material item match what the recipe requires.
	expectedInputs := craft.Input()
	if err := ensureUnlockedForCrafting(input); err != nil {
		return err
	}
	if !matchingStacks(input, expectedInputs[0]) {
		return fmt.Errorf("input item is not the same as expected input")
	}
	if err := ensureUnlockedForCrafting(material); err != nil {
		return err
	}
	if !matchingStacks(material, expectedInputs[1]) {
		return fmt.Errorf("material item is not the same as expected material")
	}
	if err := ensureUnlockedForCrafting(template); err != nil {
		return err
	}
	if !matchingStacks(template, expectedInputs[2]) {
		return fmt.Errorf("template item is not the same as expected template")
	}

	// Create the output using the input stack as reference and the recipe's output item type.
	_ = t.set(t.named(Slot{Inv: t.o.Inv.UI, Index: UISmithingInput}, RoleSmithingInput), input.Grow(-1))
	_ = t.set(t.named(Slot{Inv: t.o.Inv.UI, Index: UISmithingMaterial}, RoleSmithingMaterial), material.Grow(-1))
	_ = t.set(t.named(Slot{Inv: t.o.Inv.UI, Index: UISmithingTemplate}, RoleSmithingTemplate), template.Grow(-1))

	// Only one input item is consumed above, so only one may be produced: input.WithItem keeps the count of the whole
	// input stack.
	result := input.Grow(1 - input.Count())
	if _, ok := craft.(recipe.SmithingTrim); ok {
		var trim item.ArmourTrim
		// st, not t, which is the batch.
		if st, ok := template.Item().(item.SmithingTemplate); ok {
			trim.Template = st.Template
		} else {
			return fmt.Errorf("template item is not a smithing template")
		}
		if trim.Material, ok = material.Item().(item.ArmourTrimMaterial); !ok {
			return fmt.Errorf("material item is not an armour trim material")
		}
		trimmable, ok := input.Item().(item.Trimmable)
		if !ok {
			return fmt.Errorf("input item is not trimmable")
		}
		return t.createResults(result.WithItem(trimmable.WithTrim(trim)))
	}
	return t.createResults(result.WithItem(craft.Output()[0].Item()))
}
