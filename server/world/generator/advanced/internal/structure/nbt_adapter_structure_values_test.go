package structure

import (
	"testing"

	"github.com/df-mc/dragonfly/server/world"
)

// TestConvertStructureBlockValues checks the world-format values that Java
// structure block modes, rotations and mirrors convert to.
func TestConvertStructureBlockValues(t *testing.T) {
	cases := []struct {
		mode, rotation, mirror   string
		data                     int32
		wantRotation, wantMirror uint8
	}{
		{"", "", "", 0, 0, 0},
		{"DATA", "NONE", "NONE", 0, 0, 0},
		{"SAVE", "CLOCKWISE_90", "FRONT_BACK", 1, 1, 1},
		{"LOAD", "CLOCKWISE_180", "LEFT_RIGHT", 2, 2, 2},
		{"CORNER", "COUNTERCLOCKWISE_90", "", 3, 3, 0},
	}
	for _, c := range cases {
		data := map[string]any{"mode": c.mode}
		if c.rotation != "" {
			data["rotation"] = c.rotation
		}
		if c.mirror != "" {
			data["mirror"] = c.mirror
		}
		out, err := convertStructureBlock(world.BlockState{Name: "minecraft:structure_block"}, data)
		if err != nil {
			t.Fatal(err)
		}
		if out["data"] != c.data || out["rotation"] != c.wantRotation || out["mirror"] != c.wantMirror || out["animationMode"] != uint8(0) {
			t.Errorf("mode %q rotation %q mirror %q: data=%v rotation=%v mirror=%v animationMode=%v", c.mode, c.rotation, c.mirror, out["data"], out["rotation"], out["mirror"], out["animationMode"])
		}
	}
}
