package world

import "testing"

func TestFallDamageSetting(t *testing.T) {
	w := Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })

	if !w.FallDamage() {
		t.Fatal("fall damage is disabled by default")
	}
	w.SetFallDamage(false)
	if w.FallDamage() {
		t.Fatal("fall damage remains enabled after SetFallDamage(false)")
	}

	var nilWorld *World
	nilWorld.SetFallDamage(true)
	if nilWorld.FallDamage() {
		t.Fatal("nil World reports fall damage enabled")
	}
}
