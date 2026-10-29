package metrics

import (
	"strings"
	"sync"
	"testing"
)

func TestExposition(t *testing.T) {
	r := &Registry{}
	c := r.Counter("adamant_test_total", "Things.")
	g := r.Gauge("adamant_test_open", "Open things.")
	h := r.Histogram("adamant_test_seconds", "Durations.", 0.1, 1)
	v := r.CounterVec("adamant_test_packets_total", "Packets.", "id", 4, Itoa)
	r.GaugeFunc("adamant_test_ratio", "A ratio.", func() float64 { return 0.5 })
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 1000 {
				c.Inc()
				v.Add(2, 1)
				h.Observe(0.05)
			}
		})
	}
	wg.Wait()
	g.Add(3)
	g.Add(-1)
	h.Observe(5)
	v.Add(9, 1) // past the bound: ignored
	var b strings.Builder
	r.Write(&b)
	out := b.String()
	for _, want := range []string{
		"# TYPE adamant_test_total counter\nadamant_test_total 8000\n",
		"adamant_test_open 2\n",
		`adamant_test_seconds_bucket{le="0.1"} 8000` + "\n",
		`adamant_test_seconds_bucket{le="1"} 8000` + "\n",
		`adamant_test_seconds_bucket{le="+Inf"} 8001` + "\n",
		"adamant_test_seconds_count 8001\n",
		`adamant_test_packets_total{id="2"} 8000` + "\n",
		"adamant_test_ratio 0.5\n",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("exposition lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, `id="0"`) {
		t.Fatal("a zero counter of a vector was written")
	}
}

func BenchmarkCounter(b *testing.B) {
	c := (&Registry{}).Counter("x", "x")
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			c.Inc()
		}
	})
}
