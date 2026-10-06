package world

import "testing"

func TestStateToRuntimeIDFillsMissingProperties(t *testing.T) {
	registry := NewBlockRegistry()
	defaults := map[string]any{
		"facing":   "east",
		"half":     "bottom",
		"power":    int32(0),
		"material": "oak",
		"corner":   "none",
	}
	registry.RegisterBlockState(BlockState{Name: "test:partial_state", Properties: defaults})
	canonical := map[string]any{
		"facing":   "north",
		"half":     "top",
		"power":    int32(7),
		"material": "birch",
		"corner":   "none",
	}
	registry.RegisterBlockState(BlockState{Name: "test:partial_state", Properties: canonical})
	registry.Finalize()

	want, ok := registry.StateToRuntimeID("test:partial_state", canonical)
	if !ok {
		t.Fatal("complete canonical state was not resolved")
	}
	got, ok := registry.StateToRuntimeID("test:partial_state", map[string]any{
		"facing":   "north",
		"half":     "top",
		"power":    int32(7),
		"material": "birch",
	})
	if !ok || got != want {
		t.Fatalf("partial state resolved to (%d, %t), want (%d, true)", got, ok, want)
	}
}

func TestStateToRuntimeIDRejectsInvalidSuppliedProperties(t *testing.T) {
	registry := NewBlockRegistry()
	registry.RegisterBlockState(BlockState{Name: "test:partial_state", Properties: map[string]any{
		"facing": "east", "corner": "none",
	}})
	registry.RegisterBlockState(BlockState{Name: "test:partial_state", Properties: map[string]any{
		"facing": "north", "corner": "none",
	}})
	registry.Finalize()

	for _, properties := range []map[string]any{
		{"facing": "up"},
		{"facing": "north", "corner": "invalid"},
		{"facing": "north", "unknown_property": "value"},
		{"facing": "north", "cornet": "none"},
		{"facing": "eeast", "corner": "non"},
	} {
		if rid, ok := registry.StateToRuntimeID("test:partial_state", properties); ok {
			t.Errorf("invalid supplied properties %#v silently resolved to runtime ID %d", properties, rid)
		}
	}
}
