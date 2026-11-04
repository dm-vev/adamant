package main

import (
	"io"
	"log/slog"
	"net"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/client/clienttest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/metrics"
	"github.com/google/uuid"
	"golang.org/x/text/language"
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

// TestExpositionOfARunningServer checks the exposition that /metrics serves
// of a running server, a player online, against the text format: the
// families of the network stack and of the server together.
func TestExpositionOfARunningServer(t *testing.T) {
	l := clienttest.NewListener()
	srv := newMetricsServer(t, func(server.Config, server.Host) (server.Listener, error) { return l, nil })
	go func() {
		for range srv.Accept() {
		}
	}()
	l.Send(&clienttest.Join{L: client.Login{
		Identity: client.Identity{Name: "Steve", UUID: uuid.New()},
		Locale:   language.English,
		Addr:     &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 19132},
	}})
	for deadline := time.Now().Add(10 * time.Second); srv.PlayerCount() == 0; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the player did not join")
		}
	}
	// A tick of the player's world after it joined.
	time.Sleep(100 * time.Millisecond)

	rec := httptest.NewRecorder()
	metrics.Handler(metrics.Default, srv.Metrics()).ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	body := rec.Body.String()
	if err := metrics.Validate(strings.NewReader(body)); err != nil {
		t.Fatalf("%v:\n%s", err, body)
	}
	for _, want := range []string{"# TYPE adamant_sessions gauge\n", "adamant_players_online 1\n", `adamant_world_ticks_total{dimension="overworld"} `} {
		if !strings.Contains(body, want) {
			t.Errorf("exposition lacks %q", want)
		}
	}
}
