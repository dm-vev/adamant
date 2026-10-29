// Package metrics counts what the network stack does and serves it in the
// text format of Prometheus. Counting takes atomics alone, never a lock or
// an allocation, so that the hot paths of the transport may count every
// datagram; the registry takes a lock only to list its metrics.
package metrics

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// Counter is a value that only goes up.
type Counter struct{ v atomic.Uint64 }

// Add adds n.
func (c *Counter) Add(n uint64) { c.v.Add(n) }

// Inc adds one.
func (c *Counter) Inc() { c.v.Add(1) }

// Load returns the value.
func (c *Counter) Load() uint64 { return c.v.Load() }

// Gauge is a value that goes up and down.
type Gauge struct{ v atomic.Int64 }

// Add adds n, which may be negative.
func (g *Gauge) Add(n int64) { g.v.Add(n) }

// Set sets the value.
func (g *Gauge) Set(n int64) { g.v.Store(n) }

// Load returns the value.
func (g *Gauge) Load() int64 { return g.v.Load() }

// Histogram counts observations into buckets of upper bounds.
type Histogram struct {
	bounds []float64
	counts []atomic.Uint64
	count  atomic.Uint64
	sum    atomic.Uint64 // float64 bits
}

func newHistogram(bounds []float64) *Histogram {
	b := slices.Clone(bounds)
	sort.Float64s(b)
	return &Histogram{bounds: b, counts: make([]atomic.Uint64, len(b))}
}

// Observe counts v.
func (h *Histogram) Observe(v float64) {
	if i, _ := slices.BinarySearch(h.bounds, v); i < len(h.counts) {
		h.counts[i].Add(1)
	}
	h.count.Add(1)
	for {
		old := h.sum.Load()
		if h.sum.CompareAndSwap(old, math.Float64bits(math.Float64frombits(old)+v)) {
			return
		}
	}
}

// CounterVec is a counter for each value of a label, the values numbered
// from zero up to a bound fixed when it is made, as packet IDs are.
type CounterVec struct {
	counters []Counter
	name     func(i int) string
}

// At returns the counter of the label value i, or nil past the bound, which
// counting through ignores.
func (v *CounterVec) At(i int) *Counter {
	if i < 0 || i >= len(v.counters) {
		return nil
	}
	return &v.counters[i]
}

// Add adds n to the counter of the label value i.
func (v *CounterVec) Add(i int, n uint64) {
	if c := v.At(i); c != nil {
		c.Add(n)
	}
}

// metric is a metric of the registry with how to write it.
type metric struct {
	name, help, typ string
	write           func(w io.Writer, name string)
}

// Registry holds metrics.
type Registry struct {
	mu      sync.Mutex
	metrics []metric
	names   map[string]bool
}

// Default is the registry the network stack counts in.
var Default = &Registry{}

func (r *Registry) add(m metric) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.names == nil {
		r.names = make(map[string]bool)
	}
	if r.names[m.name] {
		panic("metrics: " + m.name + " registered twice")
	}
	r.names[m.name] = true
	r.metrics = append(r.metrics, m)
}

// Counter registers a counter.
func (r *Registry) Counter(name, help string) *Counter {
	c := &Counter{}
	r.add(metric{name, help, "counter", func(w io.Writer, name string) { fmt.Fprintf(w, "%s %d\n", name, c.Load()) }})
	return c
}

// CounterFunc registers a counter whose value f returns when written.
func (r *Registry) CounterFunc(name, help string, f func() uint64) {
	r.add(metric{name, help, "counter", func(w io.Writer, name string) { fmt.Fprintf(w, "%s %d\n", name, f()) }})
}

// Gauge registers a gauge.
func (r *Registry) Gauge(name, help string) *Gauge {
	g := &Gauge{}
	r.add(metric{name, help, "gauge", func(w io.Writer, name string) { fmt.Fprintf(w, "%s %d\n", name, g.Load()) }})
	return g
}

// GaugeFunc registers a gauge whose value f returns when written.
func (r *Registry) GaugeFunc(name, help string, f func() float64) {
	r.add(metric{name, help, "gauge", func(w io.Writer, name string) {
		fmt.Fprintf(w, "%s %s\n", name, strconv.FormatFloat(f(), 'g', -1, 64))
	}})
}

// Histogram registers a histogram of the upper bounds passed.
func (r *Registry) Histogram(name, help string, bounds ...float64) *Histogram {
	h := newHistogram(bounds)
	r.add(metric{name, help, "histogram", func(w io.Writer, name string) {
		var cum uint64
		for i, b := range h.bounds {
			cum += h.counts[i].Load()
			fmt.Fprintf(w, "%s_bucket{le=%q} %d\n", name, strconv.FormatFloat(b, 'g', -1, 64), cum)
		}
		count := h.count.Load()
		fmt.Fprintf(w, "%s_bucket{le=\"+Inf\"} %d\n", name, count)
		fmt.Fprintf(w, "%s_sum %s\n", name, strconv.FormatFloat(math.Float64frombits(h.sum.Load()), 'g', -1, 64))
		fmt.Fprintf(w, "%s_count %d\n", name, count)
	}})
	return h
}

// CounterVec registers a counter for each value of a label up to n, named
// for the exposition by labelName and value, which gives the text of a
// value. Values whose counter is zero are left out.
func (r *Registry) CounterVec(name, help, labelName string, n int, value func(i int) string) *CounterVec {
	v := &CounterVec{counters: make([]Counter, n), name: value}
	r.add(metric{name, help, "counter", func(w io.Writer, name string) {
		for i := range v.counters {
			if c := v.counters[i].Load(); c != 0 {
				fmt.Fprintf(w, "%s{%s=%q} %d\n", name, labelName, value(i), c)
			}
		}
	}})
	return v
}

// Write writes the metrics in the text format of Prometheus.
func (r *Registry) Write(w io.Writer) {
	r.mu.Lock()
	metrics := slices.Clone(r.metrics)
	r.mu.Unlock()
	slices.SortFunc(metrics, func(a, b metric) int { return strings.Compare(a.name, b.name) })
	for _, m := range metrics {
		fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s %s\n", m.name, m.help, m.name, m.typ)
		m.write(w, m.name)
	}
}

// Handler serves the metrics of the registry.
func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		r.Write(w)
	})
}

// Itoa names a label value by its number.
func Itoa(i int) string { return strconv.Itoa(i) }
