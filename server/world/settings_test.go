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

func TestSetFallDamageUpdatesSharedWorldViewersOnce(t *testing.T) {
	settings := defaultSettings()
	provider := NopProvider{Set: settings}
	overworld := Config{Synchronous: true, Provider: provider}.New()
	nether := Config{Synchronous: true, Provider: provider, Dim: Nether}.New()
	defer overworld.Close()
	defer nether.Close()

	overworldViewer, netherViewer := new(fallDamageTestViewer), new(fallDamageTestViewer)
	overworldLoader := NewLoader(0, overworld, overworldViewer)
	netherLoader := NewLoader(0, nether, netherViewer)
	defer overworld.Do(overworldLoader.Close)
	defer nether.Do(netherLoader.Close)

	nether.SetFallDamage(false)
	nether.SetFallDamage(false)
	for name, viewer := range map[string]*fallDamageTestViewer{"overworld": overworldViewer, "nether": netherViewer} {
		if len(viewer.updates) != 1 || viewer.updates[0] {
			t.Fatalf("%s updates = %v, want [false]", name, viewer.updates)
		}
	}
	if overworld.FallDamage() {
		t.Fatal("shared overworld retained fall damage")
	}
}

type fallDamageTestViewer struct {
	NopViewer
	updates []bool
}

func (v *fallDamageTestViewer) ViewFallDamage(enabled bool) {
	v.updates = append(v.updates, enabled)
}
