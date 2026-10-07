package item

import (
	"bytes"
	_ "embed"
	"image"
	"image/png"
	"sync"

	"github.com/df-mc/dragonfly/server/item/category"
	"github.com/df-mc/dragonfly/server/world"
)

//go:embed assets/knowledge_book.png
var knowledgeBookPNG []byte

var knowledgeBookTexture = sync.OnceValue(func() image.Image {
	texture, err := png.Decode(bytes.NewReader(knowledgeBookPNG))
	if err != nil {
		panic(err)
	}
	return texture
})

// KnowledgeBook stores Java recipe keys and unlocks them when used.
type KnowledgeBook struct {
	Recipes []string
}

// MaxCount returns the stack size of a knowledge book.
func (KnowledgeBook) MaxCount() int { return 1 }

// EncodeItem uses the custom-item registry because BDS has no knowledge book.
func (KnowledgeBook) EncodeItem() (name string, meta int16) { return "adamant:knowledge_book", 0 }

func (KnowledgeBook) Name() string { return "Knowledge Book" }
func (KnowledgeBook) Texture() image.Image { return knowledgeBookTexture() }
func (KnowledgeBook) Category() category.Category { return category.Items() }

func (k KnowledgeBook) EncodeNBT() map[string]any {
	recipes := make([]any, len(k.Recipes))
	for i, recipe := range k.Recipes {
		recipes[i] = recipe
	}
	return map[string]any{"adamant:recipes": recipes}
}

func (KnowledgeBook) DecodeNBT(data map[string]any) any {
	recipes, _ := javaRecipeIDs(data["adamant:recipes"])
	return KnowledgeBook{Recipes: recipes}
}

// Use unlocks the recipes carried by the book and consumes it, including when
// the component is empty or contains recipe keys the server does not know.
func (k KnowledgeBook) Use(_ *world.Tx, user User, ctx *UseContext) bool {
	if unlocker, ok := user.(interface{ UnlockRecipes([]string) }); ok {
		unlocker.UnlockRecipes(k.Recipes)
	}
	ctx.SubtractFromCount(1)
	return true
}
