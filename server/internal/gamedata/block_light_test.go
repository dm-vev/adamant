package gamedata

import "testing"

// TestBlockLightInputs checks the light inputs exported from Java 26.3 for a
// few states: emission, dampening and shape occlusion.
func TestBlockLightInputs(t *testing.T) {
	for _, c := range []struct {
		name       string
		properties map[string]any
		emission   uint8
		dampening  uint8
		empty      bool
	}{
		{"minecraft:air", nil, 0, 0, true},
		{"minecraft:stone", nil, 0, 15, true},
		{"minecraft:glowstone", nil, 15, 15, true},
		{"minecraft:water", map[string]any{"level": int32(0)}, 0, 1, true},
		{"minecraft:oak_leaves", nil, 0, 1, true},
		{"minecraft:glass", nil, 0, 0, true},
		{"minecraft:torch", nil, 14, 0, true},
		{"minecraft:oak_slab", map[string]any{"type": "bottom"}, 0, 0, false},
	} {
		shape, err := BlockGenerationShape(c.name, c.properties)
		if err != nil {
			t.Fatal(err)
		}
		if shape.LightEmission != c.emission || shape.LightDampening != c.dampening || shape.LightEmptyShape != c.empty {
			t.Fatalf("%s: emission %d, dampening %d, empty shape %v; want %d, %d, %v", c.name, shape.LightEmission, shape.LightDampening, shape.LightEmptyShape, c.emission, c.dampening, c.empty)
		}
	}
}
