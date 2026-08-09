package recipe

import (
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft/nbt"
)

func TestSpecialRecipeTypes(t *testing.T) {
	input := []Item{item.NewStack(item.Stick{}, 1)}
	output := item.NewStack(item.Apple{}, 1)
	userData := NewUserDataShapeless(input, output, "crafting_table")
	input[0] = item.NewStack(item.Apple{}, 2)
	if got := userData.Input()[0].(item.Stack); got.Item() != (item.Stick{}) || got.Count() != 1 {
		t.Fatalf("user-data input = %#v, want one stick", got)
	}
	if got := userData.Output(); len(got) != 1 || got[0].Item() != (item.Apple{}) {
		t.Fatalf("user-data output = %#v, want one apple", got)
	}

	id := uuid.MustParse("442d85ed-8272-4543-a6f1-418f90ded05d")
	multi := NewMulti(id)
	if multi.UUID() != id || multi.Block() != "crafting_table" || len(multi.Input()) != 0 || len(multi.Output()) != 0 {
		t.Fatalf("unexpected multi recipe: %#v", multi)
	}
}

func TestVanillaCraftingDataIncludesSpecialRecipes(t *testing.T) {
	var data struct {
		Shaped            []shapedRecipe    `nbt:"shaped"`
		Shapeless         []shapelessRecipe `nbt:"shapeless"`
		UserDataShapeless []shapelessRecipe `nbt:"shulker_box"`
		Multi             []string          `nbt:"multi"`
	}
	if err := nbt.Unmarshal(vanillaCraftingData, &data); err != nil {
		t.Fatal(err)
	}
	if got, want := len(data.UserDataShapeless), 1084; got != want {
		t.Fatalf("user-data recipe count = %d, want %d", got, want)
	}
	if got, want := len(data.Multi), 14; got != want {
		t.Fatalf("multi recipe count = %d, want %d", got, want)
	}
	for _, id := range data.Multi {
		if _, err := uuid.Parse(id); err != nil {
			t.Fatalf("invalid multi recipe UUID %q: %v", id, err)
		}
	}
}

func TestOutputItemDecodesRecipeNBT(t *testing.T) {
	stack, ok := (outputItem{
		Name:  "minecraft:writable_book",
		Count: 1,
		NBTData: map[string]any{
			"pages": []any{map[string]any{"text": "recipe data"}},
		},
	}).Stack()
	if !ok {
		t.Fatal("failed to resolve recipe output")
	}
	book, ok := stack.Item().(item.BookAndQuill)
	if page, exists := book.Page(0); !ok || !exists || page != "recipe data" {
		t.Fatalf("decoded output = %#v, want recipe data page", stack.Item())
	}
}
