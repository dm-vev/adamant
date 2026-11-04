package main

import (
	"io"
	"log/slog"
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/metrics"
)

// newMetricsServer returns a server of the overworld alone, with no
// listener, listening, closed as the test ends.
func newMetricsServer(t *testing.T, listeners ...func(server.Config, server.Host) (server.Listener, error)) *server.Server {
	t.Helper()
	conf := server.Config{
		Log:           slog.New(slog.NewTextHandler(io.Discard, nil)),
		DisableNether: true, DisableEnd: true,
		Generator: func(world.Dimension, int64) world.Generator { return world.NopGenerator{} },
		Listeners: listeners,
	}
	srv := conf.New()
	srv.Listen()
	t.Cleanup(func() { _ = srv.Close() })
	return srv
}

// TestMetricsNamesDoNotCollide checks that the metrics of the network stack
// and those of a server share no name, which a scrape of both would write
// once.
func TestMetricsNamesDoNotCollide(t *testing.T) {
	srv := newMetricsServer(t)
	network := metrics.Default.Names()
	if len(network) == 0 {
		t.Fatal("the network stack registered no metric")
	}
	for _, name := range srv.Metrics().Names() {
		if slices.Contains(network, name) {
			t.Errorf("%s is a metric of the network stack and of the server", name)
		}
	}
}
