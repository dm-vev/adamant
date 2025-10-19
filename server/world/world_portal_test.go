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
