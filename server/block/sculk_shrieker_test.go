package block

import (
	"reflect"
	"testing"

	"github.com/dm-vev/adamant/server/world"
)

func TestSculkShriekerNativeNBTPreservesVibrationData(t *testing.T) {
	t.Parallel()
	nativeListener := map[string]any{"selector": map[string]any{}}
	vibrationListener := (&world.VibrationData{}).EncodeNBT()
	actor := (SculkShrieker{}).DecodeNBT(map[string]any{
		"id": "SculkShrieker", "BlockEntityVersion": int32(0), "VibrationListener": nativeListener,
		"adamant:java": map[string]any{
			"warning_level": int32(2), "listener": vibrationListener, "future_java_field": int32(7),
		},
	}).(SculkShrieker)

	encoded := actor.EncodeNBT()
	if encoded["id"] != "SculkShrieker" || encoded["BlockEntityVersion"] != int32(0) {
		t.Fatalf("native actor header = %#v", encoded)
	}
	if !reflect.DeepEqual(encoded["VibrationListener"], nativeListener) {
		t.Fatalf("native vibration listener = %#v, want %#v", encoded["VibrationListener"], nativeListener)
	}
	extraState, ok := encoded["adamant:state"].(map[string]any)
	if !ok {
		t.Fatalf("adamant:state has type %T, want compound", encoded["adamant:state"])
	}
	if _, legacy := encoded["adamant:java"]; legacy {
		t.Fatalf("encoded sculk shrieker retained legacy adamant:java key: %#v", encoded)
	}
	if extraState["warning_level"] != int32(2) || !reflect.DeepEqual(extraState["listener"], vibrationListener) || extraState["future_java_field"] != int32(7) {
		t.Fatalf("extra sidecar = %#v, want warning/listener and unknown fields preserved", extraState)
	}
	if _, exists := encoded["warning_level"]; exists {
		t.Fatalf("Java warning_level leaked into native actor root: %#v", encoded)
	}
}
