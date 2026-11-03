package effect

import "testing"

// TestNamesRoundTrip checks that every named effect type is found by its
// name, with and without the minecraft namespace, and that every type
// registered by the package has a name.
func TestNamesRoundTrip(t *testing.T) {
	if len(names) != 37 || len(typeNames) != len(names) {
		t.Fatalf("%d names of %d types, want 37 of 37", len(names), len(typeNames))
	}
	for name, typ := range names {
		if got, ok := Name(typ); !ok || got != name {
			t.Errorf("Name(%T) = %q, %v, want %q, true", typ, got, ok, name)
		}
		for _, n := range []string{name, "minecraft:" + name} {
			if got, ok := ByName(n); !ok || got != typ {
				t.Errorf("ByName(%q) = %T, %v, want %T, true", n, got, ok, typ)
			}
		}
	}
	for id, typ := range effects {
		if _, ok := Name(typ); !ok {
			t.Errorf("effect %d (%T) has no name", id, typ)
		}
	}
	if _, ok := ByName("luck"); ok {
		t.Error("ByName found luck, which the game does not implement")
	}
}
