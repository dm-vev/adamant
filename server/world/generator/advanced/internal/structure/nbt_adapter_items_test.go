package structure

import "testing"

// Java's ContainerHelper.loadAllItems skips a stack that ItemStack.CODEC
// refuses, such as one of more than 99, and loads the rest of the container.
func TestConvertItemsSkipsRefusedCounts(t *testing.T) {
	items := []any{
		map[string]any{"Slot": int8(0), "id": "minecraft:stone", "count": int32(100)},
		map[string]any{"Slot": int8(1), "id": "minecraft:dirt", "count": int32(3)},
	}
	out, err := convertItems(items)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].(map[string]any)["Name"] != "minecraft:dirt" {
		t.Fatalf("converted %v, want the dirt only", out)
	}
	shelf, err := convertChiseledBookshelfItems([]any{
		map[string]any{"Slot": int8(0), "id": "minecraft:book", "count": int32(100)},
		map[string]any{"Slot": int8(2), "id": "minecraft:book", "count": int32(1)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if shelf[0].(map[string]any)["Name"] != "" || shelf[2].(map[string]any)["Name"] != "minecraft:book" {
		t.Fatalf("bookshelf converted %v, want slot 2 only", shelf)
	}
}

// ContainerHelper.loadAllItems reads Items with ItemStackWithSlot.CODEC
// through a list that drops each entry the codec refuses and keeps the
// rest: an unknown item ID, a component ItemStack.CODEC cannot read, a
// Slot that is no number, an entry that is no compound. Items that are no
// list are no items (ValueInput.listOrEmpty).
func TestConvertItemsSkipsRefusedEntries(t *testing.T) {
	items := []any{
		map[string]any{"Slot": int8(0), "id": "minecraft:no_such_item", "count": int32(1)},
		map[string]any{"Slot": int8(1), "id": "minecraft:dirt", "count": int32(3)},
		map[string]any{"Slot": int8(2), "id": "minecraft:ominous_bottle", "count": int32(1), "components": map[string]any{"minecraft:ominous_bottle_amplifier": int32(9)}},
		map[string]any{"Slot": "three", "id": "minecraft:stone", "count": int32(1)},
		int32(4),
		map[string]any{"Slot": int8(5), "id": "minecraft:stone", "count": int32(2)},
	}
	out, err := convertItems(items)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 || out[0].(map[string]any)["Name"] != "minecraft:dirt" || out[1].(map[string]any)["Slot"] != uint8(5) {
		t.Fatalf("converted %v, want the dirt and the stone of slot 5", out)
	}
	if out, err := convertItems(int32(1)); err != nil || len(out) != 0 {
		t.Fatalf("Items of an int converted to %v, %v; want none", out, err)
	}
	shelf, err := convertChiseledBookshelfItems([]any{
		map[string]any{"Slot": int8(0), "id": "minecraft:no_such_book", "count": int32(1)},
		map[string]any{"Slot": int8(2), "id": "minecraft:book", "count": int32(1)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if shelf[0].(map[string]any)["Name"] != "" || shelf[2].(map[string]any)["Name"] != "minecraft:book" {
		t.Fatalf("bookshelf converted %v, want slot 2 only", shelf)
	}
}

// Java loads the book of a lectern, the item of a decorated pot, of a
// brushable block and of an item frame with input.read(key,
// ItemStack.CODEC).orElse(ItemStack.EMPTY): a stack it refuses leaves the
// block empty, and the block loads.
func TestRefusedSingleStacksLeaveTheBlockEmpty(t *testing.T) {
	unknown := map[string]any{"id": "minecraft:no_such_item", "count": int32(1)}
	if got := javaStackOrEmpty(unknown); !got.Empty() {
		t.Fatalf("an unknown item decoded to %v", got)
	}
	if got := javaStackOrEmpty(int32(1)); !got.Empty() {
		t.Fatalf("an int decoded to %v", got)
	}
	pot := map[string]any{"item": unknown}
	if err := convertDecoratedPot(pot); err != nil {
		t.Fatal(err)
	}
	if _, ok := pot["item"]; ok {
		t.Fatalf("pot holds %v, want nothing", pot["item"])
	}
	brushable := map[string]any{"item": unknown}
	if err := convertBrushable(brushable); err != nil {
		t.Fatal(err)
	}
	if _, ok := brushable["Item"]; ok {
		t.Fatalf("brushable block holds %v, want nothing", brushable["Item"])
	}
}
