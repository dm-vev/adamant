package world

import (
	"reflect"
	"testing"
)

// TestMergeIntoNativeEntityNBTMergesAsMerge checks that merging into a map
// gives what MergeNativeEntityNBT gives, for compounds, lists merged by
// identity, an item snapshot dropping its tag and keys of one side only,
// and that the result shares nothing with the canonical map.
func TestMergeIntoNativeEntityNBTMergesAsMerge(t *testing.T) {
	retained := func() map[string]any {
		return map[string]any{
			"Pos":        []float32{1, 2, 3},
			"tag":        map[string]any{"Damage": int32(3)},
			"kept":       map[string]any{"a": int32(1), "b": []any{int32(1)}},
			"Attributes": []any{map[string]any{"Name": "minecraft:health", "Base": float32(20), "Extra": int8(1)}},
			"Armor":      []any{map[string]any{"Name": "minecraft:air", "Count": uint8(0)}},
			"only":       []byte{1, 2},
		}
	}
	for _, canonical := range []map[string]any{
		{
			"Pos":        []float32{4, 5, 6},
			"kept":       map[string]any{"b": []any{int32(2)}, "c": "x"},
			"Attributes": []any{map[string]any{"Name": "minecraft:health", "Base": float32(10)}, map[string]any{"Name": "minecraft:movement"}},
			"Armor":      []any{map[string]any{"Name": "minecraft:iron_helmet", "Count": uint8(1)}},
			"new":        map[string]any{"z": []int32{1}},
		},
		{"Name": "minecraft:stone", "Count": uint8(4)},
	} {
		want := MergeNativeEntityNBT(retained(), canonical)
		got := mergeIntoNativeEntityNBT(retained(), canonical)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("merged into:\n%#v\nwant:\n%#v", got, want)
		}
		if n, ok := canonical["new"].(map[string]any); ok {
			n["z"].([]int32)[0] = 9
			if got["new"].(map[string]any)["z"].([]int32)[0] != 1 {
				t.Error("the merge shares a value of the canonical map")
			}
		}
	}
}
