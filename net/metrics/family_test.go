package metrics

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// TestFamiliesAtScrape checks that families write the samples their collect
// functions emit, with their labels, escaped, and that families and their
// registries are written sorted by name.
func TestFamiliesAtScrape(t *testing.T) {
	r := &Registry{}
	h := NewHistogram(0.05, 0.01)
	h.Observe(0.005)
	h.Observe(0.05)
	h.Observe(3)
	r.HistogramFamily("adamant_test_tick_seconds", "Ticks.", func(emit func(*Histogram, ...string)) {
		emit(h, "dimension", "overworld")
		emit(NewHistogram(1), "dimension", `a "b"\c`+"\nd")
	})
	r.GaugeFamily("adamant_test_entities", "Entities\nby world.", func(emit func(float64, ...string)) {
		emit(3, "dimension", "overworld")
		emit(0.5, "dimension", "nether")
		emit(1)
	})
	r.CounterFamily("adamant_test_faults_total", `Faults \ kinds.`, func(emit func(uint64, ...string)) {
		emit(2, "dimension", "end", "kind", "entity")
	})
	var b strings.Builder
	r.Write(&b)
	want := `# HELP adamant_test_entities Entities\nby world.
# TYPE adamant_test_entities gauge
adamant_test_entities{dimension="overworld"} 3
adamant_test_entities{dimension="nether"} 0.5
adamant_test_entities 1
# HELP adamant_test_faults_total Faults \\ kinds.
# TYPE adamant_test_faults_total counter
adamant_test_faults_total{dimension="end",kind="entity"} 2
# HELP adamant_test_tick_seconds Ticks.
# TYPE adamant_test_tick_seconds histogram
adamant_test_tick_seconds_bucket{dimension="overworld",le="0.01"} 1
adamant_test_tick_seconds_bucket{dimension="overworld",le="0.05"} 2
adamant_test_tick_seconds_bucket{dimension="overworld",le="+Inf"} 3
adamant_test_tick_seconds_sum{dimension="overworld"} 3.055
adamant_test_tick_seconds_count{dimension="overworld"} 3
adamant_test_tick_seconds_bucket{dimension="a \"b\"\\c\nd",le="1"} 0
adamant_test_tick_seconds_bucket{dimension="a \"b\"\\c\nd",le="+Inf"} 0
adamant_test_tick_seconds_sum{dimension="a \"b\"\\c\nd"} 0
adamant_test_tick_seconds_count{dimension="a \"b\"\\c\nd"} 0
`
	if b.String() != want {
		t.Errorf("exposition:\n%s\nwant:\n%s", b.String(), want)
	}
	if err := Validate(strings.NewReader(b.String())); err != nil {
		t.Error(err)
	}
}

// TestExpositionIsValid checks Validate against the exposition of every
// kind of metric, and against expositions a scraper refuses.
func TestExpositionIsValid(t *testing.T) {
	r := &Registry{}
	r.Counter("adamant_a_total", "A.").Add(3)
	r.Gauge("adamant_b", "B.").Set(-2)
	r.Histogram("adamant_c_seconds", "C.", 0.1, 1).Observe(0.5)
	r.CounterVec("adamant_d_total", "D.", "id", 3, Itoa).Add(1, 4)
	r.GaugeFunc("adamant_e", "E.", func() float64 { return 0.25 })
	r.CounterFunc("adamant_f_total", "F.", func() uint64 { return 7 })
	other := &Registry{}
	other.Gauge("adamant_0_first", "First.")
	other.Gauge("adamant_b", "B of another registry.").Set(9)

	rec := httptest.NewRecorder()
	Handler(r, other).ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	body := rec.Body.String()
	if err := Validate(strings.NewReader(body)); err != nil {
		t.Fatalf("%v:\n%s", err, body)
	}
	if !strings.HasPrefix(body, "# HELP adamant_0_first ") || strings.Contains(body, "another registry") || !strings.Contains(body, "adamant_b -2\n") {
		t.Errorf("registries not merged, sorted, the first of a name kept:\n%s", body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain; version=0.0.4") {
		t.Errorf("Content-Type %q", ct)
	}

	for _, bad := range []string{
		"adamant_x 1\n",
		"# HELP adamant_x X.\n# TYPE adamant_x gauge\nadamant_x 1\n# HELP adamant_x X.\n",
		"# HELP adamant_x X.\n# TYPE adamant_x gauge\nadamant_y 1\n",
		"# HELP adamant_x X.\n# TYPE adamant_x gauge\nadamant_x{a=\"\\q\"} 1\n",
		"# HELP adamant_x X.\n# TYPE adamant_x gauge\nadamant_x{a=\"b} 1\n",
		"# HELP adamant_x X.\n# TYPE adamant_x gauge\nadamant_x one\n",
		"# HELP adamant_x X.\n# TYPE adamant_x counter\nadamant_x -1\n",
		"# HELP adamant_h H.\n# TYPE adamant_h histogram\nadamant_h_bucket{le=\"1\"} 2\nadamant_h_bucket{le=\"+Inf\"} 1\nadamant_h_count 1\n",
		"# HELP adamant_h H.\n# TYPE adamant_h histogram\nadamant_h_bucket{le=\"1\"} 1\nadamant_h_count 1\n",
		"# HELP adamant_h H.\n# TYPE adamant_h histogram\nadamant_h_bucket{le=\"+Inf\"} 2\nadamant_h_count 1\n",
	} {
		if Validate(strings.NewReader(bad)) == nil {
			t.Errorf("Validate passed %q", bad)
		}
	}
}

// TestNamesOfRegistry checks Names.
func TestNamesOfRegistry(t *testing.T) {
	r := &Registry{}
	r.Gauge("b", "B.")
	r.Counter("a", "A.")
	if got := strings.Join(r.Names(), ","); got != "a,b" {
		t.Errorf("Names = %s", got)
	}
}
