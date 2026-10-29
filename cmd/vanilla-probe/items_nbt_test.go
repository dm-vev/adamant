package main

import (
	"reflect"
	"testing"

	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/sandertv/gophertunnel/minecraft/nbt"
)

func TestEncodeItemsNBTRoundTrip(t *testing.T) {
	entries := []v2193.ItemEntry{
		{Name: "minecraft:negative_id", RuntimeID: -32768, Version: 1},
		{Name: "minecraft:zero_id", RuntimeID: 0, ComponentBased: true, Version: 2, Data: map[string]any{
			"components": map[string]any{
				"minecraft:food":       map[string]any{"nutrition": int32(4), "can_always_eat": uint8(1)},
				"minecraft:tags":       []any{"minecraft:is_food", "minecraft:is_meat"},
				"minecraft:durability": map[string]any{"max_durability": int32(80)},
			},
		}},
		{Name: "minecraft:positive_id", RuntimeID: 32767, Version: 3},
	}
	encoded, err := encodeItemsNBT(entries)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]vanillaItem
	if err := nbt.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded) != len(entries) {
		t.Fatalf("decoded %d entries, want %d", len(decoded), len(entries))
	}
	for _, entry := range entries {
		got, ok := decoded[entry.Name]
		if !ok {
			t.Fatalf("missing item %q", entry.Name)
		}
		if got.RuntimeID != int32(entry.RuntimeID) || got.ComponentBased != entry.ComponentBased || got.Version != entry.Version {
			t.Fatalf("metadata for %q = %+v", entry.Name, got)
		}
		if !reflect.DeepEqual(got.Data, entry.Data) {
			t.Fatalf("component data for %q = %#v, want %#v", entry.Name, got.Data, entry.Data)
		}
	}
}

func TestEncodeItemsNBTRejectsDuplicates(t *testing.T) {
	for _, test := range []struct {
		name    string
		entries []v2193.ItemEntry
	}{
		{name: "name", entries: []v2193.ItemEntry{{Name: "minecraft:stone", RuntimeID: 1}, {Name: "minecraft:stone", RuntimeID: 2}}},
		{name: "runtime ID", entries: []v2193.ItemEntry{{Name: "minecraft:stone", RuntimeID: -1}, {Name: "minecraft:dirt", RuntimeID: -1}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := encodeItemsNBT(test.entries); err == nil {
				t.Fatal("duplicate registry entry was accepted")
			}
		})
	}
}
