package item_test

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	_ "github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/dm-vev/adamant/net/nbt"
)

// Java source-only enchantments must survive native inventory NBT without
// converting stored book enchantments into active enchantments.
func TestJava263EnchantmentComponentsSurviveInventoryNBT(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_enchantment_stacks.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		Name  string         `json:"name"`
		Stack map[string]any `json:"stack"`
	}
	if err = json.Unmarshal(data, &rows); err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		t.Run(row.Name, func(t *testing.T) {
			stack, err := item.StackFromStructureNBT(row.Stack)
			if err != nil {
				t.Fatal(err)
			}
			binary, err := nbt.MarshalEncoding(item.WriteNBT(stack, true), nbt.LittleEndian)
			if err != nil {
				t.Fatal(err)
			}
			var decoded map[string]any
			if err = nbt.UnmarshalEncoding(binary, &decoded, nbt.LittleEndian); err != nil {
				t.Fatal(err)
			}
			restored := item.ReadNBT(decoded, nil)
			encoded, err := item.StackToStructureNBT(restored)
			if err != nil {
				t.Fatal(err)
			}
			normalizedJSON, err := json.Marshal(encoded)
			if err != nil {
				t.Fatal(err)
			}
			var normalized map[string]any
			if err = json.Unmarshal(normalizedJSON, &normalized); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(normalized, row.Stack) {
				t.Fatalf("persisted Java stack = %#v, want source %#v", normalized, row.Stack)
			}
		})
	}
}
