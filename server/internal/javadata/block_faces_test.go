package javadata

import "testing"

func TestGenerationFaceStateBoundaries(t *testing.T) {
	// Captured from executed Java 26.3 support/collision shapes and SupportType.FULL.
	for _, tc := range []struct {
		name               string
		properties         map[string]any
		attachment, sturdy uint8
	}{
		{"stone", nil, 63, 63},
		{"oak_leaves", nil, 63, 0},
		{"oak_slab", map[string]any{"type": "bottom"}, 1, 1},
		{"oak_slab", map[string]any{"type": "top"}, 2, 2},
		{"oak_slab", map[string]any{"type": "double"}, 63, 63},
		{"snow", map[string]any{"layers": int32(7)}, 1, 1},
		{"snow", map[string]any{"layers": int32(8)}, 63, 63},
		{"oak_trapdoor", map[string]any{"facing": "north", "open": true}, 8, 8},
		{"oak_trapdoor", map[string]any{"facing": "south", "open": true}, 4, 4},
		{"oak_trapdoor", map[string]any{"facing": "west", "open": true}, 32, 32},
		{"oak_trapdoor", map[string]any{"facing": "east", "open": true}, 16, 16},
		{"oak_trapdoor", map[string]any{"half": "top", "open": false}, 2, 2},
		{"oak_trapdoor", map[string]any{"half": "bottom", "open": false}, 1, 1},
	} {
		shape, err := BlockGenerationShape(tc.name, tc.properties)
		if err != nil || shape.Attachment != tc.attachment || shape.Sturdy != tc.sturdy {
			t.Errorf("%s %v: masks=%d/%d, want %d/%d, error=%v", tc.name, tc.properties, shape.Attachment, shape.Sturdy, tc.attachment, tc.sturdy, err)
		}
	}
}

func TestGenerationCollisionSupportAndReplacement(t *testing.T) {
	// Executed source results distinguish support faces from collision volume
	// and collision emptiness from replaceability.
	for _, tc := range []struct {
		name string
		properties map[string]any
		center uint8
		full, empty, replaceable bool
	}{
		{"air", nil, 0, false, true, true},
		{"stone", nil, 63, true, false, false},
		{"dandelion", nil, 0, false, true, false},
		{"snow", map[string]any{"layers": int32(1)}, 1, false, true, true},
		{"snow", map[string]any{"layers": int32(2)}, 1, false, false, true},
		{"snow", map[string]any{"layers": int32(8)}, 63, false, false, true},
		{"oak_fence", nil, 3, false, false, false},
	} {
		shape, err := BlockGenerationShape(tc.name, tc.properties)
		if err != nil || shape.Center != tc.center || shape.CollisionFull != tc.full ||
			shape.CollisionEmpty != tc.empty || shape.Replaceable != tc.replaceable {
			t.Errorf("%s %v: got %+v error=%v, want center=%d full=%v empty=%v replaceable=%v",
				tc.name, tc.properties, shape, err, tc.center, tc.full, tc.empty, tc.replaceable)
		}
	}
}
