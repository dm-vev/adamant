package server

import (
	"fmt"
	"runtime/debug"
	rtmetrics "runtime/metrics"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/df-mc/dragonfly/server/version"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/metrics"
)

// checkpointReasons are the reasons a checkpoint is saved for, as its
// metrics label them.
var checkpointReasons = []string{"autosave", "save-all", "close"}

// checkpointBounds are the upper bounds, in seconds, of the buckets of the
// time the phases of a checkpoint take.
var checkpointBounds = []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30}

// serverStats is what the server counts of itself for its metrics, beside
// what its worlds count.
type serverStats struct {
	// faults holds a counter for each kind of fault, by world.
	faultsMu sync.Mutex
	faults   map[*world.World]*[world.FaultFatal + 1]metrics.Counter

	joins, leaves metrics.Counter

	// checkpoints and checkpointFailures count by the index of the
	// reason in checkpointReasons, the last one for any other reason.
	checkpoints, checkpointFailures [4]metrics.Counter
	capture, write                  *metrics.Histogram
	lastChunks, lastPlayers         atomic.Int64
}

// faultCounter returns the hook of world.Config.Fault that counts the faults
// of a world by kind.
func (s *serverStats) faultCounter() (*[world.FaultFatal + 1]metrics.Counter, func(world.Fault)) {
	counts := new([world.FaultFatal + 1]metrics.Counter)
	return counts, func(f world.Fault) {
		if int(f.Kind) < len(counts) {
			counts[f.Kind].Inc()
		}
	}
}

// reasonIndex returns the index of a reason of a checkpoint in the counters
// of serverStats.
func reasonIndex(reason string) int {
	for i, r := range checkpointReasons {
		if r == reason {
			return i
		}
	}
	return len(checkpointReasons)
}

// reasonLabel returns the label of the counter of index i.
func reasonLabel(i int) string {
	if i < len(checkpointReasons) {
		return checkpointReasons[i]
	}
	return "other"
}

// Metrics returns the registry of the metrics of the server: of its worlds,
// its checkpoints, its players and listeners, of the build and of the Go
// runtime. Each Server has its own, so that many Servers in one process
// register no name twice; serve it with that of the network stack,
// metrics.Default, through metrics.Handler. Writing it reads atomics, and
// takes no lock a world owner holds.
func (srv *Server) Metrics() *metrics.Registry { return srv.metrics }

// registerMetrics makes the registry of the metrics of the server.
func (srv *Server) registerMetrics() {
	r := &metrics.Registry{}
	srv.metrics = r
	s := &srv.stats
	s.capture, s.write = metrics.NewHistogram(checkpointBounds...), metrics.NewHistogram(checkpointBounds...)

	srv.registerWorldMetrics(r)

	r.HistogramFamily("adamant_checkpoint_seconds", "Time a phase of a checkpoint took: capture on the world owners, write off them.", func(emit func(*metrics.Histogram, ...string)) {
		emit(s.capture, "phase", "capture")
		emit(s.write, "phase", "write")
	})
	reasons := func(c *[4]metrics.Counter) func(emit func(uint64, ...string)) {
		return func(emit func(uint64, ...string)) {
			for i := range c {
				if n := c[i].Load(); n != 0 || i < len(checkpointReasons) {
					emit(n, "reason", reasonLabel(i))
				}
			}
		}
	}
	r.CounterFamily("adamant_checkpoints_total", "Checkpoints saved, by reason.", reasons(&s.checkpoints))
	r.CounterFamily("adamant_checkpoint_failures_total", "Checkpoints that failed, wholly or in part, by reason.", reasons(&s.checkpointFailures))
	r.GaugeFunc("adamant_checkpoint_last_success_timestamp_seconds", "Unix time the last checkpoint that wrote everything finished, 0 if none did.", func() float64 {
		if t := srv.checkpoints.lastSuccess.Load(); t != 0 {
			return float64(t) / 1e9
		}
		return 0
	})
	r.GaugeFunc("adamant_checkpoint_chunks", "Columns the last checkpoint wrote.", func() float64 { return float64(s.lastChunks.Load()) })
	r.GaugeFunc("adamant_checkpoint_players", "Players the last checkpoint saved.", func() float64 { return float64(s.lastPlayers.Load()) })

	r.GaugeFunc("adamant_players_online", "Players online.", func() float64 { return float64(srv.PlayerCount()) })
	r.GaugeFunc("adamant_players_max", "The most players the server accepts, 0 for no limit.", func() float64 { return float64(srv.conf.MaxPlayers) })
	r.CounterFunc("adamant_player_joins_total", "Players that joined.", s.joins.Load)
	r.CounterFunc("adamant_player_leaves_total", "Players that left.", s.leaves.Load)
	r.GaugeFamily("adamant_listener_up", "1 while a listener of the server accepts clients.", func(emit func(float64, ...string)) {
		for i, l := range srv.listeners {
			emit(boolFloat(srv.listening[i].Load()), "listener", strconv.Itoa(i), "kind", listenerKind(l))
		}
	})

	info, _ := debug.ReadBuildInfo()
	goVersion, revision, mainVersion := "unknown", "", ""
	if info != nil {
		goVersion, mainVersion = info.GoVersion, info.Main.Version
		for _, set := range info.Settings {
			if set.Key == "vcs.revision" {
				revision = set.Value
			}
		}
	}
	r.GaugeFamily("adamant_build_info", "The build of the server, as labels: always 1.", func(emit func(float64, ...string)) {
		emit(1, "version", mainVersion, "minecraft", version.Minecraft, "go", goVersion, "commit", revision)
	})
	r.GaugeFunc("adamant_start_time_seconds", "Unix time the server started listening, 0 before.", func() float64 {
		if t := srv.started.Load(); t != nil {
			return float64(t.UnixNano()) / 1e9
		}
		return 0
	})
	registerRuntimeMetrics(r)
}

// registerWorldMetrics registers the metrics of the worlds of the server into
// r, each labelled with the dimension of its world.
func (srv *Server) registerWorldMetrics(r *metrics.Registry) {
	s := &srv.stats
	worlds := func(f func(w *world.World, dim string)) {
		for _, w := range srv.distinctWorlds() {
			f(w, dimensionLabel(w.Dimension()))
		}
	}
	worldHistogram := func(name, help string, h func(*world.Stats) *metrics.Histogram) {
		r.HistogramFamily(name, help, func(emit func(*metrics.Histogram, ...string)) {
			worlds(func(w *world.World, dim string) { emit(h(w.Stats()), "dimension", dim) })
		})
	}
	worldCounter := func(name, help string, v func(*world.World) uint64) {
		r.CounterFamily(name, help, func(emit func(uint64, ...string)) {
			worlds(func(w *world.World, dim string) { emit(v(w), "dimension", dim) })
		})
	}
	worldGauge := func(name, help string, v func(*world.World) float64) {
		r.GaugeFamily(name, help, func(emit func(float64, ...string)) {
			worlds(func(w *world.World, dim string) { emit(v(w), "dimension", dim) })
		})
	}
	worldHistogram("adamant_world_tick_seconds", "Time a tick of the world took.", func(s *world.Stats) *metrics.Histogram { return s.Ticks })
	worldCounter("adamant_world_ticks_total", "Ticks of the world.", func(w *world.World) uint64 { return w.Stats().Ticks.Count() })
	worldCounter("adamant_world_ticks_skipped_total", "Ticks a world too far behind skipped.", func(w *world.World) uint64 { return w.Stats().TicksSkipped.Load() })
	worldGauge("adamant_world_tps", "Ticks a second of the world, over the last 20 ticks.", func(w *world.World) float64 { return w.TPS() })
	worldGauge("adamant_world_mspt", "Mean time of the last 100 ticks of the world, in seconds.", func(w *world.World) float64 {
		avg, _ := w.TickTimes()
		return avg.Seconds()
	})
	worldGauge("adamant_world_frozen", "1 if the ticking of the world is frozen.", func(w *world.World) float64 { return boolFloat(w.Frozen()) })
	worldGauge("adamant_world_entities", "Entities in the world.", func(w *world.World) float64 { return float64(w.EntityCount()) })
	worldGauge("adamant_world_chunks_loaded", "Columns loaded in the world.", func(w *world.World) float64 { return float64(w.LoadedChunkCount()) })
	worldCounter("adamant_world_chunks_read_total", "Columns read from the world provider.", func(w *world.World) uint64 { return w.Stats().ChunksRead.Load() })
	worldCounter("adamant_world_chunk_load_errors_total", "Columns the world provider failed to read.", func(w *world.World) uint64 { return w.Stats().ChunkLoadErrors.Load() })
	worldCounter("adamant_world_chunks_unloaded_total", "Columns unloaded.", func(w *world.World) uint64 { return w.Stats().ChunksUnloaded.Load() })
	worldCounter("adamant_world_chunks_written_total", "Columns written to the world provider.", func(w *world.World) uint64 { return w.Stats().ChunksWritten.Load() })
	worldGauge("adamant_world_generation_queue", "Columns waiting for a generator worker.", func(w *world.World) float64 { return float64(w.GenerationQueueLength()) })
	worldGauge("adamant_world_generation_workers_busy", "Generator workers generating a column.", func(w *world.World) float64 { return float64(w.Stats().GenerationBusy.Load()) })
	worldHistogram("adamant_world_generation_seconds", "Time the generation of a column took.", func(s *world.Stats) *metrics.Histogram { return s.Generation })
	worldCounter("adamant_world_generation_failures_total", "Generations of columns that panicked.", func(w *world.World) uint64 { return w.Stats().GenerationFailures.Load() })
	worldCounter("adamant_world_generation_backlog_total", "Columns queued for generation past the size of the queue.", func(w *world.World) uint64 { return w.GeneratorBacklogs() })
	worldGauge("adamant_world_quarantined", "Offenders of the world quarantined after their faults.", func(w *world.World) float64 { return float64(w.QuarantinedCount()) })
	r.CounterFamily("adamant_world_faults_total", "Panics of game code the world recovered from, by kind.", func(emit func(uint64, ...string)) {
		worlds(func(w *world.World, dim string) {
			s.faultsMu.Lock()
			counts := s.faults[w]
			s.faultsMu.Unlock()
			if counts == nil {
				return
			}
			for k := range counts {
				if n := counts[k].Load(); n != 0 {
					emit(n, "dimension", dim, "kind", strings.ReplaceAll(world.FaultKind(k).String(), " ", "_"))
				}
			}
		})
	})
}

// listenerKind returns the type of a listener, as "*bedrockadapter.listener".
func listenerKind(l Listener) string { return fmt.Sprintf("%T", l) }

// boolFloat returns 1 for true and 0 for false.
func boolFloat(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// runtimeSamples are the samples of package runtime/metrics the registry
// writes, by the names the Go collector of Prometheus gives them.
var runtimeSamples = []struct{ sample, name, help, typ string }{
	{"/sched/goroutines:goroutines", "go_goroutines", "Goroutines that currently exist.", "gauge"},
	{"/sched/gomaxprocs:threads", "go_sched_gomaxprocs_threads", "The current runtime.GOMAXPROCS setting.", "gauge"},
	{"/memory/classes/heap/objects:bytes", "go_memory_classes_heap_objects_bytes", "Memory occupied by live objects and dead objects not yet freed.", "gauge"},
	{"/memory/classes/total:bytes", "go_memory_classes_total_bytes", "All memory mapped by the Go runtime into the address space.", "gauge"},
	{"/gc/cycles/total:gc-cycles", "go_gc_cycles_total_gc_cycles_total", "Completed GC cycles.", "counter"},
}

// registerRuntimeMetrics registers the samples of runtimeSamples on r,
// read at each scrape with runtime/metrics, which stops no goroutine, unlike
// runtime.ReadMemStats.
func registerRuntimeMetrics(r *metrics.Registry) {
	var mu sync.Mutex
	samples := make([]rtmetrics.Sample, len(runtimeSamples))
	for i, s := range runtimeSamples {
		samples[i].Name = s.sample
	}
	var read time.Time
	value := func(i int) uint64 {
		mu.Lock()
		defer mu.Unlock()
		// The samples of one scrape are read once.
		if time.Since(read) > time.Second {
			rtmetrics.Read(samples)
			read = time.Now()
		}
		if samples[i].Value.Kind() == rtmetrics.KindUint64 {
			return samples[i].Value.Uint64()
		}
		return 0
	}
	for i, s := range runtimeSamples {
		if s.typ == "counter" {
			r.CounterFunc(s.name, s.help, func() uint64 { return value(i) })
			continue
		}
		r.GaugeFunc(s.name, s.help, func() float64 { return float64(value(i)) })
	}
}

// dimensionLabel returns the name of a dimension as the logs and the metrics
// of the server spell it: "overworld", "nether", "end".
func dimensionLabel(dim world.Dimension) string {
	return strings.ToLower(fmt.Sprint(dim))
}
