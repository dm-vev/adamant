package main

import (
	"io"
	"log"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/logging"
	"github.com/dm-vev/adamant/net/metrics"
)

// TestLogLinesDroppedAreCounted checks that the lines the rate limit of a
// Set drops are counted by component in adamant_log_lines_dropped_total.
func TestLogLinesDroppedAreCounted(t *testing.T) {
	drops := newLogDrops()
	r := &metrics.Registry{}
	drops.register(r)
	set := logging.New(logging.Options{RateLimit: 1, Logger: log.New(io.Discard, "", 0), Dropped: drops.add})
	for range 25 {
		set.Logger(logging.Session).Info("flood")
		set.Logger("plugin").Info("flood")
	}
	var b strings.Builder
	r.Write(&b)
	for _, want := range []string{
		`adamant_log_lines_dropped_total{component="session"} 15` + "\n",
		`adamant_log_lines_dropped_total{component="world"} 0` + "\n",
		`adamant_log_lines_dropped_total{component="plugin"} 15` + "\n",
	} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("metrics lack %q:\n%s", want, b.String())
		}
	}
	if err := metrics.Validate(strings.NewReader(b.String())); err != nil {
		t.Error(err)
	}
}
