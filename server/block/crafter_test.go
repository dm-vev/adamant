package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/recipe"
	"github.com/df-mc/dragonfly/server/world"
)

func TestCrafterPersistsDisabledSlotsAndInventory(t *testing.T) {
	crafter := NewCrafter()
	_ = crafter.inventory.SetItem(4, item.NewStack(item.Diamond{}, 3))
	crafter, changed := crafter.SetSlotDisabled(2, true)
	if !changed {
		t.Fatal("empty slot did not become disabled")
	}
	data := crafter.EncodeNBT()
	restored := (Crafter{Facing: crafter.Facing, Top: crafter.Top}).DecodeNBT(data).(Crafter)
	stack, _ := restored.inventory.Item(4)
	if stack.Count() != 3 || restored.DisabledSlots != 1<<2 {
		t.Fatalf("restored items=%v disabled=%09b", stack, restored.DisabledSlots)
	}
	if _, changed := restored.SetSlotDisabled(4, true); changed {
		t.Fatal("non-empty slot was disabled")
	}
}

func TestCrafterCraftsAndInsertsRegisteredRecipe(t *testing.T) {
	recipe.Register(recipe.NewShapeless([]recipe.Item{item.NewStack(item.Diamond{}, 1)}, item.NewStack(item.Apple{}, 1), "crafting_table"))
	crafter := NewCrafter()
	crafter.Facing = cube.FaceEast
	_ = crafter.inventory.SetItem(6, item.NewStack(item.Diamond{}, 2))
	barrel := NewBarrel()
	worldConfig := world.Config{Synchronous: true}.New()
	defer worldConfig.Close()
	pos := cube.Pos{0, 1, 0}
	<-worldConfig.Exec(func(tx *world.Tx) {
		tx.SetBlock(pos, crafter, nil)
		tx.SetBlock(pos.Side(cube.FaceEast), barrel, nil)
		crafter.ScheduledTick(pos, tx, nil)
		got := tx.Block(pos).(Crafter)
		input, _ := got.inventory.Item(6)
		outputs := tx.Block(pos.Side(cube.FaceEast)).(Barrel).inventory.Items()
		if input.Count() != 1 || len(outputs) != 1 || outputs[0].Item() != (item.Apple{}) || outputs[0].Count() != 1 || !got.Crafting || got.CraftingTicksRemaining != 6 {
			t.Fatalf("input=%v output=%v crafting=%v ticks=%d", input, outputs, got.Crafting, got.CraftingTicksRemaining)
		}
	})
}
func TestCrafterShapedEmptyCells(t *testing.T) {
	ingredients := make([]recipe.Item, 9)
	input := make([]item.Stack, 9)
	for slot := range 9 {
		if slot != 4 {
			input[slot] = item.NewStack(Cobblestone{}, 1)
		}
		ingredients[slot] = input[slot]
	}
	r := recipe.NewShaped(ingredients, item.NewStack(Furnace{}, 1), recipe.Shape{3, 3}, "crafting_table")
	output, ok := matchCrafterShaped(r, input)
	if !ok {
		t.Fatal("empty centre cell prevented the furnace recipe from matching")
	}
	if name, _ := output.Item().EncodeItem(); name != "minecraft:furnace" || output.Count() != 1 {
		t.Fatalf("unexpected furnace output %v", output)
	}
	input[4] = item.NewStack(item.IronIngot{}, 1)
	if _, ok := matchCrafterShaped(r, input); ok {
		t.Fatal("occupied centre cell matched a required empty recipe cell")
	}
}

func TestHopperFeedsEnabledCrafterSlot(t *testing.T) {
	crafter := NewCrafter()
	crafter, _ = crafter.SetSlotDisabled(0, true)
	crafter, _ = crafter.SetSlotDisabled(2, true)
	hopper := NewHopper()
	_ = hopper.inventory.SetItem(0, item.NewStack(item.Diamond{}, 2))

	w := world.Config{Synchronous: true}.New()
	defer w.Close()
	pos := cube.Pos{0, 1, 0}
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(pos, crafter, nil)
		if !crafter.InsertItem(hopper, pos, tx) {
			t.Fatal("hopper did not insert into an enabled Crafter slot")
		}
		got := tx.Block(pos).(Crafter)
		enabled, _ := got.inventory.Item(1)
		disabled, _ := got.inventory.Item(0)
		remaining, _ := hopper.inventory.Item(0)
		if enabled.Count() != 1 || !disabled.Empty() || remaining.Count() != 1 {
			t.Fatalf("enabled=%v disabled=%v hopper=%v", enabled, disabled, remaining)
		}
	})
}

func TestAllCrafterStatesEncodeDistinctOrientations(t *testing.T) {
	states := allCrafters()
	if len(states) != 48 {
		t.Fatalf("got %d Crafter block states, want 48", len(states))
	}
	seen := make(map[[3]any]bool, len(states))
	for _, state := range states {
		crafter := state.(Crafter)
		name, properties := crafter.EncodeBlock()
		key := [3]any{properties["orientation"], properties["triggered_bit"], properties["crafting"]}
		if name != "minecraft:crafter" || seen[key] {
			t.Fatalf("invalid or duplicate Crafter state %q %v", name, key)
		}
		seen[key] = true
	}
}
