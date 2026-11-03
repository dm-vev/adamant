package main

import (
	"reflect"
	"testing"

	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/dm-vev/adamant/net/nbt"
)

func TestEncodeBlocksNBTRoundTrip(t *testing.T) {
	entries := []v2193.BlockEntry{
		{Name: "minecraft:white_wool_stairs", Properties: map[string]any{
			"blockTags":     []any{"minecraft:is_shears_item_destructible"},
			"molangVersion": int32(13),
			"vanilla_block_data": map[string]any{
				"block_archetype": map[string]any{"base_block": "minecraft:white_wool", "type": "stair_block"},
				"block_id":        int32(1405),
				"translucency":    float32(0.8),
			},
		}},
		{Name: "minecraft:red_shrub", Properties: map[string]any{"molangVersion": int32(13)}},
	}
	encoded, err := encodeBlocksNBT(entries)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]map[string]any
	if err := nbt.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded) != len(entries) {
		t.Fatalf("decoded %d entries, want %d", len(decoded), len(entries))
	}
	for _, entry := range entries {
		if got := decoded[entry.Name]; !reflect.DeepEqual(got, entry.Properties) {
			t.Fatalf("properties of %q = %#v, want %#v", entry.Name, got, entry.Properties)
		}
	}
}

func TestEncodeBlocksNBTRejectsBadNames(t *testing.T) {
	for _, test := range []struct {
		name    string
		entries []v2193.BlockEntry
	}{
		{name: "empty", entries: []v2193.BlockEntry{{Name: ""}}},
		{name: "duplicate", entries: []v2193.BlockEntry{{Name: "minecraft:red_shrub"}, {Name: "minecraft:red_shrub"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := encodeBlocksNBT(test.entries); err == nil {
				t.Fatal("bad block entry was accepted")
			}
		})
	}
}
