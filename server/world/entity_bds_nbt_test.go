package world

import "testing"

// TestEntityNBTReadsBDSRotationAndName checks entities from a BDS world keep
// their rotation and name, which BDS saves as Rotation and CustomName.
func TestEntityNBTReadsBDSRotationAndName(t *testing.T) {
	for _, rotation := range []any{[]float32{90, -10}, []any{float32(90), float32(-10)}} {
		h := entityFromData(closeTrackingEntityType{}, 1, map[string]any{"Rotation": rotation, "CustomName": "Bob", "CustomNameVisible": uint8(0)})
		if h.data.Rot.Yaw() != 90 || h.data.Rot.Pitch() != -10 {
			t.Fatalf("rotation %T decoded to %v, want 90 -10", rotation, h.data.Rot)
		}
		if h.data.Name != "Bob" || h.data.AlwaysShowNameTag {
			t.Fatalf("name decoded to %q always=%v", h.data.Name, h.data.AlwaysShowNameTag)
		}
		saved := h.encodeNBT()
		if r, ok := saved["Rotation"].([]float32); !ok || r[0] != 90 || r[1] != -10 || saved["CustomName"] != "Bob" {
			t.Fatalf("saved as Rotation %v CustomName %v", saved["Rotation"], saved["CustomName"])
		}
	}
}
