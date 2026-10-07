package item

import (
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/nbt"
)

func TestKnowledgeBookNativePersistenceRetainsJavaIdentity(t *testing.T) {
	recipes := []string{"minecraft:oak_planks", "minecraft:crafting_table"}
	stack, err := StackFromJavaNBT(map[string]any{
		"id": "minecraft:knowledge_book", "count": int32(1),
		"components": map[string]any{"minecraft:recipes": []any{recipes[0], recipes[1]}},
	})
	if err != nil { t.Fatal(err) }
	bookID, _, ok := world.ItemRuntimeID(stack.Item())
	nativeBookID, _, _ := world.ItemRuntimeID(Book{})
	if !ok || bookID == nativeBookID { t.Fatal("knowledge book was not registered as a distinct item") }
	native := WriteNBT(stack, true)
	if native["Name"] != "adamant:knowledge_book" { t.Fatalf("native item identifier: %v", native["Name"]) }
	encoded, err := nbt.Marshal(native)
	if err != nil { t.Fatal(err) }
	var decoded map[string]any
	if err := nbt.Unmarshal(encoded, &decoded); err != nil { t.Fatal(err) }
	restored := ReadNBT(decoded, nil)
	book, ok := restored.Item().(KnowledgeBook)
	if !ok || !slices.Equal(book.Recipes, recipes) { t.Fatalf("restored item: %#v", restored.Item()) }
	source, err := StackToJavaNBT(restored)
	if err != nil { t.Fatal(err) }
	if source["id"] != "minecraft:knowledge_book" { t.Fatalf("Java item identifier: %v", source["id"]) }
}
