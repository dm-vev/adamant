package block

import "testing"

func TestSkullRetainsCapturedNativeAnimation(t *testing.T) {
	// BDS 1.26.52.3 build51798919: a dragon head beside a redstone block,
	// and an unpowered control, saved from a ticking area without clients.
	for _, tc := range []struct {
		name    string
		powered byte
		ticks   int32
	}{
		{"powered", 1, 115}, {"unpowered", 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			captured := map[string]any{"id": "Skull", "SkullType": byte(255), "Rotation": float32(0), "DoingAnimation": tc.powered, "MouthTickCount": tc.ticks, "BlockEntityVersion": int32(0)}
			restored := (Skull{Type: DragonHead()}).DecodeNBT(captured).(Skull)
			if restored.Powered != (tc.powered != 0) {
				t.Fatal("native DoingAnimation did not restore powered state")
			}
			saved := restored.EncodeNBT()
			if saved["DoingAnimation"] != tc.powered || saved["MouthTickCount"] != tc.ticks {
				t.Fatalf("native animation fields lost on save: %#v", saved)
			}
			if _, obsolete := saved["MouthMoving"]; obsolete {
				t.Fatal("saved obsolete, non-native animation key")
			}
		})
	}
}
