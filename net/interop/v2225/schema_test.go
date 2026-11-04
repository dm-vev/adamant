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
// it writes schema.txt of v2225, with the corrections after Mojang's
// documentation that still apply to it.
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
	if !bytes.Equal(got, want) {
		t.Fatal("v2225/schema.txt is not what protoimport imports: run it")
	}
	if !strings.Contains(string(got), "\n# Corrected after Mojang's protocol documentation: AutoCraftRecipeStackRequestAction") {
		t.Fatal("the ingredients of recipe-book crafts are not corrected")
	}
}
