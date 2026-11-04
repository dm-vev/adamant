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
