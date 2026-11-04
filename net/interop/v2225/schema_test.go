package v2225

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestSchemaIsImported runs protoimport against the gophertunnel of 2225:
// it writes schema.txt of v2225 but for the hand edit of the ingredients
// of recipe-book crafts, which v2193 has too.
func TestSchemaIsImported(t *testing.T) {
	out := filepath.Join(t.TempDir(), "schema.txt")
	if b, err := exec.Command("go", "tool", "protoimport", "-o", out).CombinedOutput(); err != nil {
		t.Fatalf("protoimport: %v\n%s", err, b)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("../../bedrock/proto/v2225/schema.txt")
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(got), "# 479 declarations, 12 of them manual.", "# 480 declarations, 13 of them manual.", 1)
	edited = strings.Replace(edited, "type AutoCraftRecipeStackRequestAction\n\tRecipeNetworkID varuint32\n\tNumberOfCrafts uint8\n\tIngredients []ItemDescriptorCount\n",
		"type RecipeIngredient\n\tmanual statement id, _, ok := itemDescriptorType(x.Descriptor)\n\ntype AutoCraftRecipeStackRequestAction\n\tRecipeNetworkID varuint32\n\tNumberOfCrafts uint8\n\tIngredients []RecipeIngredient\n", 1)
	if edited == string(got) || !bytes.Equal([]byte(edited), want) {
		t.Fatal("v2225/schema.txt is not what protoimport imports with the hand edit of RecipeIngredient: run it")
	}
}
