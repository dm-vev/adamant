package world

import (
	"fmt"
	"testing"
)

func TestWorldPortalDestinationNil(t *testing.T) {
	conf := Config{
		Dim:                   Overworld,
		PortalDestination:     func(Dimension) *World { return nil },
		PortalDisabledMessage: func(dim Dimension) string { return fmt.Sprintf("%v disabled", dim) },
	}
	w := conf.New()
	defer w.Close()

	if dest := w.PortalDestination(Nether); dest != nil {
		t.Fatalf("expected nil destination when portal disabled, got %v", dest)
	}
	expected := fmt.Sprintf("%v disabled", Nether)
	if msg := w.PortalDisabledMessage(Nether); msg != expected {
		t.Fatalf("unexpected disabled message: got %q, want %q", msg, expected)
	}
}

func TestWorldPortalDestinationAvoidsSelf(t *testing.T) {
	fallback := Config{Dim: Overworld}.New()
	t.Cleanup(func() { _ = fallback.Close() })

	var w *World
	conf := Config{
		Dim: Nether,
		PortalDestination: func(Dimension) *World {
			return w
		},
		DefaultWorld: func() *World {
			return fallback
		},
	}
	w = conf.New()
	defer w.Close()

	if dest := w.PortalDestination(Nether); dest != fallback {
		t.Fatalf("expected portal destination to fall back when resolver returns source world, got %v", dest)
	}
}
