package server

import (
	"context"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/internal/faultcheck"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/metrics"
)

// TestServerMetrics checks the metrics of a server with a player online:
// its worlds tick, its faults and its checkpoints are counted, and its
// listeners, players and build are written, as an exposition a scraper
// reads.
func TestServerMetrics(t *testing.T) {
	jt := newJoinTest(t, player.NopProvider{}, Config{})
	jt.start()
	jt.join(t, joinLogin("Alice", ""))

	w := jt.srv.World()
	if err := w.Do(func(*world.Tx) { panic(faultcheck.Expected{Why: "a fault for the metrics"}) }).Wait(t.Context()); err == nil {
		t.Fatal("a transaction that panicked did not fail")
	}
	if err := jt.srv.checkpoints.checkpoint(context.Background(), "save-all"); err != nil {
		t.Fatal(err)
	}
	ticks := w.Stats().Ticks.Count()
	if ticks == 0 {
		t.Fatal("the world counted no tick")
	}

	var b strings.Builder
	metrics.WriteAll(&b, jt.srv.Metrics())
	out := b.String()
	if err := metrics.Validate(strings.NewReader(out)); err != nil {
		t.Fatalf("%v:\n%s", err, out)
	}
	for _, want := range []string{
		`adamant_world_faults_total{dimension="overworld",kind="transaction"} 1` + "\n",
		`adamant_checkpoints_total{reason="save-all"} 1` + "\n",
		`adamant_checkpoints_total{reason="autosave"} 0` + "\n",
		`adamant_checkpoint_failures_total{reason="save-all"} 0` + "\n",
		`adamant_checkpoint_seconds_count{phase="capture"} 1` + "\n",
		`adamant_checkpoint_seconds_count{phase="write"} 1` + "\n",
		"adamant_checkpoint_players 1\n",
		"adamant_players_online 1\n",
		"adamant_player_joins_total 1\n",
		"adamant_player_leaves_total 0\n",
		`adamant_listener_up{listener="0",kind="*clienttest.Listener"} 1` + "\n",
		`adamant_world_tick_seconds_bucket{dimension="overworld",le="0.05"} `,
		`adamant_world_ticks_total{dimension="overworld"} `,
		`adamant_world_entities{dimension="overworld"} 1` + "\n",
		`adamant_build_info{version="`,
		"go_goroutines ",
		"go_gc_cycles_total_gc_cycles_total ",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("metrics lack %q", want)
		}
	}
	if strings.Contains(out, `dimension="nether"`) {
		t.Error("metrics of a disabled dimension")
	}
	if jt.srv.checkpoints.lastSuccess.Load() == 0 || strings.Contains(out, "adamant_checkpoint_last_success_timestamp_seconds 0\n") {
		t.Error("the checkpoint is not the last success")
	}
}

// TestMetricsOfManyServers checks that every Server has a registry of its
// own, so that tests that make many register no name twice.
func TestMetricsOfManyServers(t *testing.T) {
	servers := make([]*Server, 2)
	for i := range servers {
		jt := newJoinTest(t, player.NopProvider{}, Config{})
		jt.start()
		t.Cleanup(func() { _ = jt.srv.Close() })
		servers[i] = jt.srv
	}
	a, b := servers[0], servers[1]
	if a.Metrics() == b.Metrics() || len(a.Metrics().Names()) == 0 {
		t.Fatal("two servers share a registry, or have none")
	}
	for _, name := range a.Metrics().Names() {
		if !strings.HasPrefix(name, "adamant_") && !strings.HasPrefix(name, "go_") {
			t.Errorf("metric %s of no prefix of the server", name)
		}
	}
}
