package main

import (
	"sync"

	"github.com/df-mc/dragonfly/server/logging"
	"github.com/dm-vev/adamant/net/metrics"
)

// logDrops counts the log lines the rate limit of the logs dropped, by
// component: a counter of each component the Set of loggers knows, read
// without a lock, and those of other names under one.
type logDrops struct {
	known map[string]*metrics.Counter

	mu    sync.Mutex
	other map[string]*metrics.Counter
}

// newLogDrops returns the counters of the lines dropped.
func newLogDrops() *logDrops {
	d := &logDrops{known: map[string]*metrics.Counter{}, other: map[string]*metrics.Counter{}}
	for _, c := range logging.Components() {
		d.known[c] = &metrics.Counter{}
	}
	return d
}

// add counts n lines of a component dropped: the Dropped of
// logging.Options.
func (d *logDrops) add(component string, n uint64) {
	if c, ok := d.known[component]; ok {
		c.Add(n)
		return
	}
	d.mu.Lock()
	c, ok := d.other[component]
	if !ok {
		c = &metrics.Counter{}
		d.other[component] = c
	}
	d.mu.Unlock()
	c.Add(n)
}

// register registers adamant_log_lines_dropped_total on r.
func (d *logDrops) register(r *metrics.Registry) {
	r.CounterFamily("adamant_log_lines_dropped_total", "Log lines the rate limit of the logs dropped, by component.", func(emit func(uint64, ...string)) {
		for _, c := range logging.Components() {
			emit(d.known[c].Load(), "component", c)
		}
		d.mu.Lock()
		defer d.mu.Unlock()
		for name, c := range d.other {
			emit(c.Load(), "component", name)
		}
	})
}
