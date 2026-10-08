package block

import (
	"reflect"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
)

func TestSculkShriekerNativeNBTPreservesJavaVibrationData(t *testing.T) {
	nativeListener := map[string]any{"selector": map[string]any{}}
	javaListener := (&world.VibrationData{}).EncodeNBT()
	actor := (SculkShrieker{}).DecodeNBT(map[string]any{
		"id": "SculkShrieker", "BlockEntityVersion": int32(0), "VibrationListener": nativeListener,
		"adamant:java": map[string]any{
			"warning_level": int32(2), "listener": javaListener, "future_java_field": int32(7),
		},
	}).(SculkShrieker)

	encoded := actor.EncodeNBT()
	if encoded["id"] != "SculkShrieker" || encoded["BlockEntityVersion"] != int32(0) {
		t.Fatalf("native actor header = %#v", encoded)
	}
	if !reflect.DeepEqual(encoded["VibrationListener"], nativeListener) {
		t.Fatalf("native vibration listener = %#v, want %#v", encoded["VibrationListener"], nativeListener)
	}
	javaState, ok := encoded["adamant:java"].(map[string]any)
	if !ok {
		t.Fatalf("adamant:java has type %T, want compound", encoded["adamant:java"])
	}
	if javaState["warning_level"] != int32(2) || !reflect.DeepEqual(javaState["listener"], javaListener) || javaState["future_java_field"] != int32(7) {
		t.Fatalf("Java sidecar = %#v, want warning/listener and unknown fields preserved", javaState)
	}
	if _, exists := encoded["warning_level"]; exists {
		t.Fatalf("Java warning_level leaked into native actor root: %#v", encoded)
	}
}
