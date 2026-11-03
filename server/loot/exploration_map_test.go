package loot

import (
	"encoding/json"
	"io/fs"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
)

// TestEveryLootMapDecorationHasABedrockType walks the loot tables for the
// map decorations their exploration maps name: each has a Bedrock type, or
// the chest that rolls the map fails to fill, as an abandoned camp's did.
func TestEveryLootMapDecorationHasABedrockType(t *testing.T) {
	names := map[string]string{}
	err := fs.WalkDir(assets, "assets/minecraft/loot_table", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := assets.ReadFile(path)
		if err != nil {
			return err
		}
		var v any
		if err := json.Unmarshal(b, &v); err != nil {
			return err
		}
		collectDecorations(v, path, names)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(names) == 0 {
		t.Fatal("no loot table names a map decoration")
	}
	for name, path := range names {
		if _, ok := world.MapDecorationTypeFromJava(name); !ok {
			t.Errorf("%s names map decoration %q, which has no Bedrock type", path, name)
		}
	}
}

// collectDecorations records the decoration of every function of v that
// names one, by the table it is in.
func collectDecorations(v any, path string, names map[string]string) {
	switch v := v.(type) {
	case map[string]any:
		if d, ok := v["decoration"].(string); ok {
			names[d] = path
		}
		for _, x := range v {
			collectDecorations(x, path, names)
		}
	case []any:
		for _, x := range v {
			collectDecorations(x, path, names)
		}
	}
}
