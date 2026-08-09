package item

import "testing"

func TestLodestoneCompassNBTRoundTrip(t *testing.T) {
	want := Compass{TrackingHandle: 42}
	data := WriteNBT(NewStack(want, 1), true)
	got := ReadNBT(data, nil)
	compass, ok := got.Item().(Compass)
	if !ok || compass != want {
		t.Fatalf("decoded compass = %#v, want %#v", got.Item(), want)
	}
	if name, _ := compass.EncodeItem(); name != "minecraft:lodestone_compass" || !compass.Glinted() {
		t.Fatalf("decoded compass was not linked: name=%q glinted=%v", name, compass.Glinted())
	}
}

func TestLodestoneCompassWithoutHandleDecodesAsCompass(t *testing.T) {
	got := Compass{TrackingHandle: 1}.DecodeNBT(nil).(Compass)
	if got.TrackingHandle != 0 {
		t.Fatalf("tracking handle = %d, want 0", got.TrackingHandle)
	}
	if name, _ := got.EncodeItem(); name != "minecraft:compass" {
		t.Fatalf("item name = %q, want minecraft:compass", name)
	}
}
