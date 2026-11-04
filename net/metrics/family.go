package metrics

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
)

// NewHistogram returns a histogram of the upper bounds passed that no
// registry writes by itself: the collect function of a HistogramFamily
// writes it, with its labels.
func NewHistogram(bounds ...float64) *Histogram { return newHistogram(bounds) }

// Count returns the number of observations.
func (h *Histogram) Count() uint64 { return h.count.Load() }

// Sum returns the sum of the observations.
func (h *Histogram) Sum() float64 { return math.Float64frombits(h.sum.Load()) }

// write writes the buckets, the sum and the count of h, as the samples of
// the histogram name with the labels passed, which are written as they are
// before le.
func (h *Histogram) write(w io.Writer, name, labels string) {
	sep := ""
	if labels != "" {
		sep = ","
	}
	var cum uint64
	for i, b := range h.bounds {
		cum += h.counts[i].Load()
		fmt.Fprintf(w, "%s_bucket{%s%sle=\"%s\"} %d\n", name, labels, sep, strconv.FormatFloat(b, 'g', -1, 64), cum)
	}
	// The count is read after the buckets: an observation counted between
	// the two reads makes +Inf no less than the last bucket.
	count := max(h.count.Load(), cum)
	fmt.Fprintf(w, "%s_bucket{%s%sle=\"+Inf\"} %d\n", name, labels, sep, count)
	fmt.Fprintf(w, "%s_sum%s %s\n", name, braces(labels), strconv.FormatFloat(h.Sum(), 'g', -1, 64))
	fmt.Fprintf(w, "%s_count%s %d\n", name, braces(labels), count)
}

// braces returns labels in braces, or nothing if there are none.
func braces(labels string) string {
	if labels == "" {
		return ""
	}
	return "{" + labels + "}"
}

// formatLabels returns the label pairs passed, name then value, as the text
// format writes them inside braces: name="value", joined by commas, the
// values escaped. A last name without a value is left out.
func formatLabels(pairs []string) string {
	var b strings.Builder
	for i := 0; i+1 < len(pairs); i += 2 {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(pairs[i])
		b.WriteString(`="`)
		b.WriteString(EscapeLabel(pairs[i+1]))
		b.WriteByte('"')
	}
	return b.String()
}

// labelEscaper escapes label values as the text format of Prometheus does.
var labelEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)

// helpEscaper escapes the text of HELP lines.
var helpEscaper = strings.NewReplacer(`\`, `\\`, "\n", `\n`)

// EscapeLabel escapes a label value as the text format of Prometheus does:
// a backslash, a double quote and a newline. Other bytes are written as
// they are.
func EscapeLabel(v string) string { return labelEscaper.Replace(v) }

// GaugeFamily registers a gauge with labels, whose samples collect emits
// each time the registry is written: a value with its labels, name then
// value. collect must not hold a lock the game holds on a hot path.
func (r *Registry) GaugeFamily(name, help string, collect func(emit func(v float64, labels ...string))) {
	r.add(metric{name, help, "gauge", func(w io.Writer, name string) {
		collect(func(v float64, labels ...string) {
			fmt.Fprintf(w, "%s%s %s\n", name, braces(formatLabels(labels)), strconv.FormatFloat(v, 'g', -1, 64))
		})
	}})
}

// CounterFamily registers a counter with labels, whose samples collect
// emits each time the registry is written.
func (r *Registry) CounterFamily(name, help string, collect func(emit func(v uint64, labels ...string))) {
	r.add(metric{name, help, "counter", func(w io.Writer, name string) {
		collect(func(v uint64, labels ...string) {
			fmt.Fprintf(w, "%s%s %d\n", name, braces(formatLabels(labels)), v)
		})
	}})
}

// HistogramFamily registers a histogram with labels, whose histograms, made
// with NewHistogram, collect emits each time the registry is written.
func (r *Registry) HistogramFamily(name, help string, collect func(emit func(h *Histogram, labels ...string))) {
	r.add(metric{name, help, "histogram", func(w io.Writer, name string) {
		collect(func(h *Histogram, labels ...string) {
			if h != nil {
				h.write(w, name, formatLabels(labels))
			}
		})
	}})
}

// Names returns the names of the metrics of the registry, sorted.
func (r *Registry) Names() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	names := make([]string, 0, len(r.metrics))
	for _, m := range r.metrics {
		names = append(names, m.name)
	}
	slices.Sort(names)
	return names
}

// Handler serves the metrics of the registries passed as one exposition,
// sorted by name across them. A name of two registries is written once, of
// the first that has it.
func Handler(regs ...*Registry) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		WriteAll(w, regs...)
	})
}

// WriteAll writes the metrics of the registries passed as Handler serves
// them.
func WriteAll(w io.Writer, regs ...*Registry) {
	var all []metric
	seen := map[string]bool{}
	for _, r := range regs {
		if r == nil {
			continue
		}
		r.mu.Lock()
		for _, m := range r.metrics {
			if !seen[m.name] {
				seen[m.name] = true
				all = append(all, m)
			}
		}
		r.mu.Unlock()
	}
	writeMetrics(w, all)
}

// writeMetrics writes metrics sorted by name, each with its HELP and TYPE.
func writeMetrics(w io.Writer, metrics []metric) {
	slices.SortFunc(metrics, func(a, b metric) int { return strings.Compare(a.name, b.name) })
	for _, m := range metrics {
		fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s %s\n", m.name, helpEscaper.Replace(m.help), m.name, m.typ)
		m.write(w, m.name)
	}
}
