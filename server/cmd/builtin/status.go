package builtin

import (
	"fmt"
	"runtime"
	"runtime/metrics"
	"sync"
	"time"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/world"
)

type statusCommand struct {
	srv serverAdapter
}

func newStatusCommand(srv serverAdapter) cmd.Command {
	return cmd.New("status", "Displays server performance statistics.", nil, statusCommand{srv: srv})
}

func (s statusCommand) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	w := tx.World()
	if w == nil {
		o.Error("world unavailable")
		return
	}

	start := s.srv.StartTime()
	if !start.IsZero() {
		o.Printf("Uptime: %s", time.Since(start).Round(time.Second))
	}

	playerCount := 0
	for range s.srv.Players(tx) {
		playerCount++
	}
	o.Printf("Players: %d/%d", playerCount, s.srv.MaxPlayerCount())
	o.Printf("World: %s | Chunks: %d | Entities: %d", w.Name(), w.LoadedChunkCount(), w.EntityCount())

	if tps := w.TPS(); tps > 0 {
		o.Printf("TPS (avg): %.2f / 20.00", tps)
	} else {
		o.Print("TPS (avg): collecting samples...")
	}

	if cpuLoad, ready := sampleAverageCPULoad(); ready {
		o.Printf("CPU load (per core): %.2f%% across %d cores", cpuLoad, runtime.NumCPU())
	} else {
		o.Print("CPU load: collecting baseline, try again shortly.")
	}

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	heapAlloc := bytesToMiB(mem.HeapAlloc)
	heapSys := bytesToMiB(mem.HeapSys)
	lastGC := "never"
	if mem.LastGC != 0 {
		lastGC = fmt.Sprintf("%s ago", time.Since(time.Unix(0, int64(mem.LastGC))).Round(time.Second))
	}
	o.Printf("Memory: %.2f MiB heap used / %.2f MiB reserved", heapAlloc, heapSys)
	o.Printf("Goroutines: %d | GOMAXPROCS: %d | GC cycles: %d | Last GC: %s", runtime.NumGoroutine(), runtime.GOMAXPROCS(0), mem.NumGC, lastGC)
}

var (
    cpuSampleMu       sync.Mutex
    cpuSampleLastTime time.Time
    cpuSampleLastTotal float64
    cpuSampleLastIdle  float64
)

func sampleAverageCPULoad() (float64, bool) {
    samples := []metrics.Sample{
        {Name: "/cpu/classes/total:cpu-seconds"},
        {Name: "/cpu/classes/idle:cpu-seconds"},
    }
    metrics.Read(samples)

    // Guard against unexpected value kinds to prevent panics on Float64().
    if samples[0].Value.Kind() != metrics.KindFloat64 || samples[1].Value.Kind() != metrics.KindFloat64 {
        return 0, false
    }

    total := samples[0].Value.Float64()
    idle := samples[1].Value.Float64()
    now := time.Now()

    cpuSampleMu.Lock()
    defer cpuSampleMu.Unlock()

    ready := !cpuSampleLastTime.IsZero()
    deltaTime := now.Sub(cpuSampleLastTime).Seconds()
    deltaTotal := total - cpuSampleLastTotal
    deltaIdle := idle - cpuSampleLastIdle

    cpuSampleLastTime = now
    cpuSampleLastTotal = total
    cpuSampleLastIdle = idle

    if !ready || deltaTime <= 0 || deltaTotal <= 0 || deltaIdle < 0 {
        return 0, false
    }

    // Utilization is the fraction of total CPU time that was not idle.
    usage := ((deltaTotal - deltaIdle) / deltaTotal) * 100
    if usage < 0 {
        usage = 0
    }
    if usage > 100 {
        usage = 100
    }
    return usage, true
}

func bytesToMiB(v uint64) float64 {
	return float64(v) / (1024 * 1024)
}
