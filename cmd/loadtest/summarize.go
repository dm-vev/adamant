package main

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// summary is summary.json, schema "adamant-perf-summary/1".
type summary struct {
	Schema    string    `json:"schema"`
	Generated time.Time `json:"generated"`
	Base      string    `json:"base"`
	Commits   []string  `json:"commits"`
	// Notes are caveats of whoever ran the comparison, such as a machine
	// that was busy.
	Notes []string `json:"notes"`
	// FullCommits maps the short SHAs of Commits to the full ones.
	FullCommits map[string]string `json:"full_commits"`
	Host        map[string]string `json:"host"`
	// Build holds, by commit, how its server and benchmarks were built:
	// the go version and the build environment the script recorded.
	Build map[string]map[string]string `json:"build"`

	// Metrics describes every metric of LoadTest.
	Metrics map[string]metricInfo `json:"metrics"`
	// LoadTest is commit -> scenario -> metric -> its spread over the
	// repetitions.
	LoadTest map[string]map[string]map[string]spread `json:"loadtest"`
	// Comparisons holds, for each commit but the base, scenario -> metric
	// -> how it compares with the base.
	Comparisons map[string]map[string]map[string]comparison `json:"comparisons"`

	Micro microSummary `json:"microbench"`
	Text  textSummary  `json:"text_bench"`
}

type metricInfo struct {
	Unit        string `json:"unit"`
	Better      string `json:"better"`
	Description string `json:"description"`
}

// spread is a metric over the repetitions of a scenario.
type spread struct {
	N      int       `json:"n"`
	Mean   float64   `json:"mean"`
	Stddev float64   `json:"stddev"`
	CV     float64   `json:"cv"`
	Min    float64   `json:"min"`
	Max    float64   `json:"max"`
	Values []float64 `json:"values"`
	Reps   []int     `json:"reps"`
}

type comparison struct {
	Base float64 `json:"base_mean"`
	Head float64 `json:"head_mean"`
	// DeltaPercent is (head - base) / base in percent; null if the base
	// is zero.
	DeltaPercent *float64 `json:"delta_percent"`
	// NoisePercent is the larger coefficient of variation of the two, in
	// percent: a delta well within it is noise.
	NoisePercent float64 `json:"noise_percent"`
	// RangesOverlap reports whether the min..max ranges of the
	// repetitions of the two overlap.
	RangesOverlap bool `json:"ranges_overlap"`
}

type microSummary struct {
	// Common lists the benchmarks at every commit, as package and name.
	Common [][2]string `json:"common"`
	// OnlyAt lists, by commit, the benchmarks only some commits have.
	OnlyAt map[string][][2]string `json:"only_at"`
	// Results is commit -> "package BenchmarkName" -> unit -> samples.
	Results map[string]map[string]map[string]sample `json:"results"`
	// Comparisons holds the comparisons of each commit with the base.
	Comparisons []microComparison `json:"comparisons"`
}

// sample summarises the runs of a benchmark.
type sample struct {
	N      int     `json:"n"`
	Median float64 `json:"median"`
	Mean   float64 `json:"mean"`
	Stddev float64 `json:"stddev"`
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
	values []float64
}

type microComparison struct {
	Commit    string `json:"commit"`
	Package   string `json:"package"`
	Benchmark string `json:"benchmark"`
	Unit      string `json:"unit"`
	Base      sample `json:"base"`
	Head      sample `json:"head"`
	// DeltaPercent compares the medians; P is the p-value of a
	// Mann-Whitney U test of the two sets of runs.
	DeltaPercent *float64 `json:"delta_percent"`
	P            float64  `json:"p"`
	// Benchstat holds what benchstat printed for the same pair: its delta
	// ("~" for no significant difference) and its p-value and counts.
	Benchstat *benchstatCell `json:"benchstat,omitempty"`
}

type benchstatCell struct {
	Delta string `json:"delta"`
	P     string `json:"p"`
}

type textSummary struct {
	Commit string `json:"commit"`
	// Results is "BenchmarkName/variant" -> unit -> samples.
	Results map[string]map[string]sample `json:"results"`
	// Comparisons compares the new paths with the old ones.
	Comparisons []textComparison `json:"comparisons"`
}

type textComparison struct {
	Benchmark string   `json:"benchmark"`
	Old       string   `json:"old"`
	New       string   `json:"new"`
	Unit      string   `json:"unit"`
	OldMedian float64  `json:"old_median"`
	NewMedian float64  `json:"new_median"`
	Ratio     *float64 `json:"ratio"`
	P         float64  `json:"p"`
}

// metrics are the metrics taken from each run, by name.
var metrics = []struct {
	name string
	info metricInfo
	get  func(r *runResult) (float64, bool)
}{
	{"server_cpu_percent_mean", metricInfo{"percent of one core", "lower", "mean CPU use of the server process over the window"}, func(r *runResult) (float64, bool) {
		return srv(r, func(s *serverResult) float64 { return s.Window.CPUPercent.Mean })
	}},
	{"server_cpu_percent_p95", metricInfo{"percent of one core", "lower", "95th percentile of the one-second CPU samples of the server in the window"}, func(r *runResult) (float64, bool) {
		return srv(r, func(s *serverResult) float64 { return s.Window.CPUPercent.P95 })
	}},
	{"server_cpu_seconds", metricInfo{"s", "lower", "CPU time the server used in the window"}, func(r *runResult) (float64, bool) {
		return srv(r, func(s *serverResult) float64 { return s.Window.CPUSeconds })
	}},
	{"server_rss_mib_mean", metricInfo{"MiB", "lower", "mean resident memory of the server in the window"}, func(r *runResult) (float64, bool) {
		return srv(r, func(s *serverResult) float64 { return s.Window.RSSMiB.Mean })
	}},
	{"server_rss_mib_max", metricInfo{"MiB", "lower", "largest resident memory of the server in the window"}, func(r *runResult) (float64, bool) {
		return srv(r, func(s *serverResult) float64 { return s.Window.RSSMiB.Max })
	}},
	{"server_threads_mean", metricInfo{"threads", "lower", "mean thread count of the server in the window"}, func(r *runResult) (float64, bool) {
		return srv(r, func(s *serverResult) float64 { return s.Window.Threads.Mean })
	}},
	{"server_startup_ms", metricInfo{"ms", "lower", "from starting the server process to its ready line"}, func(r *runResult) (float64, bool) { return r.StartupMs, r.StartupMs != 0 }},
	{"join_spawn_ms_p50", metricInfo{"ms", "lower", "median time from the dial to the spawn"}, func(r *runResult) (float64, bool) { return r.Join.SpawnMs.P50, r.Join.SpawnMs.N > 0 }},
	{"join_spawn_ms_p95", metricInfo{"ms", "lower", "95th percentile of the time from the dial to the spawn"}, func(r *runResult) (float64, bool) { return r.Join.SpawnMs.P95, r.Join.SpawnMs.N > 0 }},
	{"join_chunks_ms_p50", metricInfo{"ms", "lower", "median time from the dial to the last chunk of the view radius"}, func(r *runResult) (float64, bool) { return r.Join.ChunksMs.P50, r.Join.ChunksMs.N > 0 }},
	{"join_chunks_ms_p95", metricInfo{"ms", "lower", "95th percentile of the time from the dial to the last chunk of the view radius"}, func(r *runResult) (float64, bool) {
		return r.Join.ChunksMs.P95, r.Join.ChunksMs.N > 0
	}},
	{"chat_rtt_ms_p50", metricInfo{"ms", "lower", "median round trip of a chat line to its broadcast"}, func(r *runResult) (float64, bool) { return r.Chat.RTTMs.P50, r.Chat.RTTMs.N > 0 }},
	{"chat_rtt_ms_p95", metricInfo{"ms", "lower", "95th percentile of the round trip of chat"}, func(r *runResult) (float64, bool) { return r.Chat.RTTMs.P95, r.Chat.RTTMs.N > 0 }},
	{"chat_rtt_ms_p99", metricInfo{"ms", "lower", "99th percentile of the round trip of chat"}, func(r *runResult) (float64, bool) { return r.Chat.RTTMs.P99, r.Chat.RTTMs.N > 0 }},
	{"chat_lost", metricInfo{"lines", "lower", "chat lines sent in the window whose broadcast did not come back"}, func(r *runResult) (float64, bool) { return float64(r.Chat.Lost), true }},
	{"packets_per_s_per_bot", metricInfo{"packets/s", "same", "packets a bot received a second in the window"}, func(r *runResult) (float64, bool) { return r.Traffic.PacketsPerS, true }},
	{"packet_bytes_per_s_per_bot", metricInfo{"B/s", "same", "bytes of the packets a bot received a second, decompressed"}, func(r *runResult) (float64, bool) { return r.Traffic.PacketBytesPerS, true }},
	{"wire_bytes_per_s_per_bot", metricInfo{"B/s", "same", "bytes a bot received a second on the wire, compressed"}, func(r *runResult) (float64, bool) { return r.Traffic.WireBytesPerS, true }},
	{"join_failed", metricInfo{"bots", "lower", "bots that did not spawn"}, func(r *runResult) (float64, bool) { return float64(r.Join.Failed), true }},
	{"chunk_timeouts", metricInfo{"bots", "lower", "bots whose chunks did not all arrive within the chunk timeout"}, func(r *runResult) (float64, bool) { return float64(r.Join.ChunkTimeouts), true }},
	{"disconnects", metricInfo{"bots", "lower", "bots that lost their connection before the end"}, func(r *runResult) (float64, bool) { return float64(r.Errors.Disconnects), true }},
	{"decode_errors", metricInfo{"packets", "lower", "packets of the server the bots could not decode"}, func(r *runResult) (float64, bool) { return float64(r.Errors.DecodeErrors), true }},
	{"stack_requests_failed", metricInfo{"requests", "lower", "item stack requests the server refused"}, func(r *runResult) (float64, bool) { return float64(r.Actions.StackFailed), true }},
	{"host_other_cores", metricInfo{"cores", "lower", "CPU the rest of the machine used in the window, besides the server and the bots: other load"}, func(r *runResult) (float64, bool) { return r.Host.OtherCores, true }},
	{"host_bot_cores", metricInfo{"cores", "lower", "CPU the bots used in the window"}, func(r *runResult) (float64, bool) { return r.Host.BotCores, true }},
}

func srv(r *runResult, f func(*serverResult) float64) (float64, bool) {
	if r.Server == nil || r.Server.Window.CPUPercent.N == 0 {
		return 0, false
	}
	return f(r.Server), true
}

// summarize writes summary.json from a results directory.
func summarize(args []string) error {
	fs := flag.NewFlagSet("summarize", flag.ExitOnError)
	dir := fs.String("results", "/home/agent/perf-results", "results directory")
	base := fs.String("base", "", "base commit, the first of -commits if empty")
	commitList := fs.String("commits", "", "commits, comma-separated, as the directories of -results name them")
	out := fs.String("out", "", "file to write; <results>/summary.json if empty")
	var notes []string
	fs.Func("note", "a caveat to record in the summary; may be repeated", func(v string) error {
		notes = append(notes, v)
		return nil
	})
	_ = fs.Parse(args)
	commits := strings.Split(*commitList, ",")
	if *commitList == "" {
		return errors.New("summarize: -commits is required")
	}
	if *base == "" {
		*base = commits[0]
	}
	if *out == "" {
		*out = filepath.Join(*dir, "summary.json")
	}
	s := summary{
		Schema: "adamant-perf-summary/1", Generated: time.Now().UTC(), Base: *base, Commits: commits, Notes: append([]string{}, notes...),
		FullCommits: map[string]string{}, Host: hostInfo(), Build: map[string]map[string]string{},
		Metrics: map[string]metricInfo{}, LoadTest: map[string]map[string]map[string]spread{},
		Comparisons: map[string]map[string]map[string]comparison{},
	}
	for _, m := range metrics {
		s.Metrics[m.name] = m.info
	}
	for _, c := range commits {
		if b, err := os.ReadFile(filepath.Join(*dir, c, "commit.txt")); err == nil {
			s.FullCommits[c] = strings.TrimSpace(string(b))
		}
		s.Build[c] = readBuild(filepath.Join(*dir, c))
		lt, err := loadRuns(filepath.Join(*dir, c, "loadtest"))
		if err != nil {
			return err
		}
		s.LoadTest[c] = lt
	}
	for _, c := range commits {
		if c == *base {
			continue
		}
		s.Comparisons[c] = map[string]map[string]comparison{}
		for scenario, ms := range s.LoadTest[c] {
			bs, ok := s.LoadTest[*base][scenario]
			if !ok {
				continue
			}
			s.Comparisons[c][scenario] = map[string]comparison{}
			for name, h := range ms {
				b, ok := bs[name]
				if !ok {
					continue
				}
				cmp := comparison{Base: b.Mean, Head: h.Mean, NoisePercent: 100 * max(b.CV, h.CV), RangesOverlap: b.Min <= h.Max && h.Min <= b.Max}
				if b.Mean != 0 {
					d := 100 * (h.Mean - b.Mean) / b.Mean
					cmp.DeltaPercent = &d
				}
				s.Comparisons[c][scenario][name] = cmp
			}
		}
	}
	var err error
	if s.Micro, err = microBench(*dir, *base, commits); err != nil {
		return err
	}
	if s.Text, err = textBench(*dir); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(*out, append(data, '\n'), 0o644)
}

// readBuild reads what the script recorded of the build of a commit:
// go-version.txt, and build.txt, which holds a key=value a line.
func readBuild(dir string) map[string]string {
	b := map[string]string{}
	if data, err := os.ReadFile(filepath.Join(dir, "go-version.txt")); err == nil {
		b["go_version"] = strings.TrimSpace(string(data))
	}
	if data, err := os.ReadFile(filepath.Join(dir, "build.txt")); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if k, v, ok := strings.Cut(line, "="); ok {
				b[strings.TrimSpace(k)] = strings.TrimSpace(v)
			}
		}
	}
	return b
}

// loadRuns reads the runs below dir, one directory a scenario.
func loadRuns(dir string) (map[string]map[string]spread, error) {
	out := map[string]map[string]spread{}
	files, _ := filepath.Glob(filepath.Join(dir, "*", "rep*.json"))
	sort.Strings(files)
	type point struct {
		rep int
		v   float64
	}
	values := map[string]map[string][]point{}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		var r runResult
		if err := json.Unmarshal(data, &r); err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		scenario := filepath.Base(filepath.Dir(f))
		rep, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(filepath.Base(f), "rep"), ".json"))
		if values[scenario] == nil {
			values[scenario] = map[string][]point{}
		}
		for _, m := range metrics {
			if v, ok := m.get(&r); ok {
				values[scenario][m.name] = append(values[scenario][m.name], point{rep, v})
			}
		}
	}
	for scenario, ms := range values {
		out[scenario] = map[string]spread{}
		for name, ps := range ms {
			sort.Slice(ps, func(i, j int) bool { return ps[i].rep < ps[j].rep })
			sp := spread{N: len(ps)}
			for _, p := range ps {
				sp.Values = append(sp.Values, p.v)
				sp.Reps = append(sp.Reps, p.rep)
			}
			sp.Mean, sp.Stddev, sp.Min, sp.Max = moments(sp.Values)
			if sp.Mean != 0 {
				sp.CV = sp.Stddev / math.Abs(sp.Mean)
			}
			out[scenario][name] = sp
		}
	}
	return out, nil
}

// moments returns the mean, the sample standard deviation, the minimum
// and the maximum of v.
func moments(v []float64) (mean, stddev, lo, hi float64) {
	if len(v) == 0 {
		return 0, 0, 0, 0
	}
	lo, hi = v[0], v[0]
	for _, x := range v {
		mean += x
		lo, hi = min(lo, x), max(hi, x)
	}
	mean /= float64(len(v))
	if len(v) > 1 {
		for _, x := range v {
			stddev += (x - mean) * (x - mean)
		}
		stddev = math.Sqrt(stddev / float64(len(v)-1))
	}
	return mean, stddev, lo, hi
}

func newSample(v []float64) sample {
	s := sample{N: len(v), values: v}
	s.Mean, s.Stddev, s.Min, s.Max = moments(v)
	sorted := slices.Clone(v)
	sort.Float64s(sorted)
	if len(sorted) > 0 {
		s.Median = quantile(sorted, 0.5)
	}
	return s
}

// benchLine matches a result line of go test -bench; benchName the name
// alone, when what the benchmark logs splits its line, and benchRest the
// rest of such a line.
var (
	benchLine = regexp.MustCompile(`^(Benchmark\S+?)(-\d+)?\s+\d+\s+(\d.*)$`)
	benchName = regexp.MustCompile(`^(Benchmark\S+?)(-\d+)?\s`)
	benchRest = regexp.MustCompile(`^\s+\d+\s+(\d.*)$`)
)

// readBench reads a go test -bench output: "package Name" -> unit ->
// values.
func readBench(path string) (map[string]map[string][]float64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string]map[string][]float64{}
	pkg, pending := "", ""
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if p, ok := strings.CutPrefix(line, "pkg: "); ok {
			pkg = strings.TrimSpace(p)
			continue
		}
		var name, rest string
		if m := benchLine.FindStringSubmatch(line); m != nil {
			name, rest, pending = m[1], m[3], ""
		} else if m := benchName.FindStringSubmatch(line); m != nil {
			pending = m[1]
			continue
		} else if m := benchRest.FindStringSubmatch(line); m != nil && pending != "" {
			name, rest, pending = pending, m[1], ""
		} else {
			continue
		}
		key := pkg + " " + name
		fields := strings.Fields(rest)
		for i := 0; i+1 < len(fields); i += 2 {
			v, err := strconv.ParseFloat(fields[i], 64)
			if err != nil {
				break
			}
			unit := fields[i+1]
			if out[key] == nil {
				out[key] = map[string][]float64{}
			}
			out[key][unit] = append(out[key][unit], v)
		}
	}
	return out, sc.Err()
}

// microBench summarises the microbenchmarks of the commits.
func microBench(dir, base string, commits []string) (microSummary, error) {
	ms := microSummary{Common: [][2]string{}, OnlyAt: map[string][][2]string{}, Results: map[string]map[string]map[string]sample{}, Comparisons: []microComparison{}}
	if data, err := os.ReadFile(filepath.Join(dir, "bench", "common.txt")); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			if f := strings.Fields(line); len(f) == 3 {
				ms.Common = append(ms.Common, [2]string{filepath.Join(f[0], f[1]), f[2]})
			}
		}
	}
	raw := map[string]map[string]map[string][]float64{}
	for _, c := range commits {
		if data, err := os.ReadFile(filepath.Join(dir, c, "bench-only-here.txt")); err == nil {
			for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
				if f := strings.Fields(line); len(f) == 3 {
					ms.OnlyAt[c] = append(ms.OnlyAt[c], [2]string{filepath.Join(f[0], f[1]), f[2]})
				}
			}
		}
		r, err := readBench(filepath.Join(dir, c, "bench", "micro.txt"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return ms, err
		}
		raw[c] = r
		ms.Results[c] = map[string]map[string]sample{}
		for key, units := range r {
			ms.Results[c][key] = map[string]sample{}
			for unit, v := range units {
				ms.Results[c][key][unit] = newSample(v)
			}
		}
	}
	for _, c := range commits {
		if c == base || raw[c] == nil || raw[base] == nil {
			continue
		}
		cells := readBenchstatCSV(filepath.Join(dir, "bench", base+"-vs-"+c+".csv"))
		keys := make([]string, 0, len(raw[c]))
		for key := range raw[c] {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			bu, ok := raw[base][key]
			if !ok {
				continue
			}
			pkg, name, _ := strings.Cut(key, " ")
			units := make([]string, 0, len(raw[c][key]))
			for unit := range raw[c][key] {
				units = append(units, unit)
			}
			sort.Strings(units)
			for _, unit := range units {
				bv, ok := bu[unit]
				if !ok {
					continue
				}
				hv := raw[c][key][unit]
				mc := microComparison{Commit: c, Package: pkg, Benchmark: name, Unit: benchstatUnit(unit), Base: newSample(bv), Head: newSample(hv), P: mannWhitney(bv, hv)}
				if mc.Base.Median != 0 {
					d := 100 * (mc.Head.Median - mc.Base.Median) / mc.Base.Median
					mc.DeltaPercent = &d
				}
				if cell, ok := cells[pkg+" "+strings.TrimPrefix(name, "Benchmark")+" "+benchstatUnit(unit)]; ok {
					mc.Benchstat = &cell
				}
				ms.Comparisons = append(ms.Comparisons, mc)
			}
		}
	}
	return ms, nil
}

// benchstatUnit returns the unit as benchstat names it.
func benchstatUnit(u string) string {
	if u == "ns/op" {
		return "sec/op"
	}
	return u
}

// readBenchstatCSV reads the output of benchstat -format csv for two
// files: "package Name unit" -> the delta and the p-value.
func readBenchstatCSV(path string) map[string]benchstatCell {
	out := map[string]benchstatCell{}
	data, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	pkg, unit := "", ""
	for _, block := range strings.Split(string(data), "\n") {
		if p, ok := strings.CutPrefix(block, "pkg: "); ok {
			pkg = strings.TrimSpace(p)
			continue
		}
		if !strings.Contains(block, ",") {
			continue
		}
		rec, err := csv.NewReader(strings.NewReader(block)).Read()
		if err != nil || len(rec) < 7 {
			continue
		}
		switch {
		case rec[0] == "" && rec[1] != "" && rec[2] == "CI":
			unit = rec[1]
		case rec[0] == "" || rec[0] == "geomean":
		default:
			name := regexp.MustCompile(`-\d+$`).ReplaceAllString(rec[0], "")
			out[pkg+" "+name+" "+unit] = benchstatCell{Delta: rec[5], P: rec[6]}
		}
	}
	return out
}

// textBench summarises the text benchmarks, old paths against new.
func textBench(dir string) (textSummary, error) {
	ts := textSummary{Results: map[string]map[string]sample{}, Comparisons: []textComparison{}}
	if b, err := os.ReadFile(filepath.Join(dir, "bench", "text-commit.txt")); err == nil {
		ts.Commit = strings.TrimSpace(string(b))
	}
	r, err := readBench(filepath.Join(dir, "bench", "text.txt"))
	if errors.Is(err, os.ErrNotExist) {
		return ts, nil
	} else if err != nil {
		return ts, err
	}
	raw := map[string]map[string][]float64{}
	for key, units := range r {
		_, name, _ := strings.Cut(key, " ")
		raw[name] = units
		ts.Results[name] = map[string]sample{}
		for unit, v := range units {
			ts.Results[name][unit] = newSample(v)
		}
	}
	for _, p := range [][3]string{
		{"BenchmarkTextPlain", "old", "new"},
		{"BenchmarkTextPlain", "old-raw", "new-legacy"},
		{"BenchmarkTextStatus", "old", "new"},
		{"BenchmarkTextStatus", "old", "new-legacy"},
		{"BenchmarkTextTranslation", "old", "new"},
	} {
		o, n := raw[p[0]+"/"+p[1]], raw[p[0]+"/"+p[2]]
		for _, unit := range []string{"ns/op", "B/op", "allocs/op"} {
			if o[unit] == nil || n[unit] == nil {
				continue
			}
			c := textComparison{Benchmark: p[0], Old: p[1], New: p[2], Unit: benchstatUnit(unit), OldMedian: newSample(o[unit]).Median, NewMedian: newSample(n[unit]).Median, P: mannWhitney(o[unit], n[unit])}
			if c.OldMedian != 0 {
				ratio := c.NewMedian / c.OldMedian
				c.Ratio = &ratio
			}
			ts.Comparisons = append(ts.Comparisons, c)
		}
	}
	return ts, nil
}

// mannWhitney returns the two-sided p-value of a Mann-Whitney U test of
// a and b, with the normal approximation corrected for ties: 1 if either
// is empty or all values are equal.
func mannWhitney(a, b []float64) float64 {
	n1, n2 := float64(len(a)), float64(len(b))
	if n1 == 0 || n2 == 0 {
		return 1
	}
	type v struct {
		x     float64
		fromA bool
	}
	all := make([]v, 0, len(a)+len(b))
	for _, x := range a {
		all = append(all, v{x, true})
	}
	for _, x := range b {
		all = append(all, v{x, false})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].x < all[j].x })
	var rankA, ties float64
	for i := 0; i < len(all); {
		j := i
		for j < len(all) && all[j].x == all[i].x {
			j++
		}
		rank := float64(i+j+1) / 2
		t := float64(j - i)
		ties += t*t*t - t
		for k := i; k < j; k++ {
			if all[k].fromA {
				rankA += rank
			}
		}
		i = j
	}
	u := rankA - n1*(n1+1)/2
	n := n1 + n2
	sigma := math.Sqrt(n1 * n2 / 12 * ((n + 1) - ties/(n*(n-1))))
	if sigma == 0 {
		return 1
	}
	z := (math.Abs(u-n1*n2/2) - 0.5) / sigma
	return math.Min(1, math.Erfc(max(z, 0)/math.Sqrt2))
}

// hostInfo describes the machine the summary is written on.
func hostInfo() map[string]string {
	h := map[string]string{"goos": runtime.GOOS, "goarch": runtime.GOARCH, "cpus": strconv.Itoa(runtime.NumCPU())}
	if data, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if k, v, ok := strings.Cut(line, ":"); ok && strings.TrimSpace(k) == "model name" {
				h["cpu"] = strings.TrimSpace(v)
				break
			}
		}
	}
	if data, err := os.ReadFile("/proc/meminfo"); err == nil {
		line, _, _ := strings.Cut(string(data), "\n")
		h["memory"] = strings.Join(strings.Fields(line)[1:], " ")
	}
	if data, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
		h["kernel"] = strings.TrimSpace(string(data))
	}
	return h
}
