package server

import (
	"io"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"log/slog"
)

func TestServerDefaultDimensionFallback(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelWarn}))
	conf := Config{
		Log:                     log,
		DisableResourceBuilding: true,
		DisableOverworld:        true,
		DisableEnd:              true,
		DefaultDimension:        world.Nether,
	}

	srv := conf.New()
	t.Cleanup(func() {
		for _, w := range srv.dimensions {
			if w != nil {
				_ = w.Close()
			}
		}
	})

	if got := srv.World().Dimension(); got != world.Nether {
		t.Fatalf("expected default dimension nether, got %v", got)
	}
	if srv.Nether() != srv.World() {
		t.Fatalf("expected nether world to match default world")
	}
	if srv.End() != nil {
		t.Fatalf("expected end world to be nil when disabled")
	}

	// Disabled dimensions should resolve to the default world.
	if got := srv.dimension(world.Overworld); got.Dimension() != world.Nether {
		t.Fatalf("expected overworld lookup to fall back to nether, got %v", got.Dimension())
	}
	if got := srv.dimension(world.End); got.Dimension() != world.Nether {
		t.Fatalf("expected end lookup to fall back to nether, got %v", got.Dimension())
	}
}
