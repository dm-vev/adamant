package world

import (
	"github.com/dm-vev/adamant/net/metrics"
)

// TickBounds are the upper bounds, in seconds, of the buckets of
// Stats.Ticks: around 50 ms, the time a tick has at 20 ticks a second, so
// that the bucket of 0.05 over the count is the share of ticks on time.
var TickBounds = []float64{0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 1}

// GenerationBounds are the upper bounds, in seconds, of the buckets of
// Stats.Generation.
var GenerationBounds = []float64{0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5}

// Stats is what a world counts of itself, for metrics: atomics the world
// adds to as it ticks, loads, generates and saves, and that a scrape reads
// at any time, from any goroutine. The world alone writes them.
type Stats struct {
	// Ticks holds the time each tick took, in seconds; its count is the
	// number of ticks.
	Ticks *metrics.Histogram
	// TicksSkipped counts the ticks a world too far behind skipped.
	TicksSkipped metrics.Counter
	// ChunksRead counts the columns read from the provider,
	// ChunkLoadErrors those it failed to read, ChunksUnloaded the columns
	// unloaded and ChunksWritten those written to the provider.
	ChunksRead, ChunkLoadErrors, ChunksUnloaded, ChunksWritten metrics.Counter
	// Generation holds the time the generation of each column took, in
	// seconds, GenerationBusy the workers generating a column now, and
	// GenerationFailures the generations that panicked.
	Generation         *metrics.Histogram
	GenerationBusy     metrics.Gauge
	GenerationFailures metrics.Counter
	// OwnerBusy counts the nanoseconds the owner of the world spent running
	// transactions: ticks, the work of players and loaders, and the rest.
	// Against the time that passed it tells how much of the owner is left.
	OwnerBusy metrics.Counter
}

// newStats returns the Stats of a new world.
func newStats() *Stats {
	return &Stats{Ticks: metrics.NewHistogram(TickBounds...), Generation: metrics.NewHistogram(GenerationBounds...)}
}

// Stats returns what the world counts of itself.
func (w *World) Stats() *Stats {
	if w == nil {
		return newStats()
	}
	return w.stats
}

// GenerationQueueLength returns the number of columns waiting for a
// generator worker.
func (w *World) GenerationQueueLength() int {
	if w == nil || w.generatorQueue == nil {
		return 0
	}
	return int(w.generatorQueue.length.Load())
}

// QueuedTransactions returns the number of transactions waiting for the
// owner of the world: the queue and its overflow.
func (w *World) QueuedTransactions() int {
	if w == nil {
		return 0
	}
	w.queueMu.Lock()
	defer w.queueMu.Unlock()
	return len(w.queue) + len(w.queueOverflow)
}

// GeneratorBacklogs returns how many times columns were queued for
// generation past the size of the queue, which the world warns of.
func (w *World) GeneratorBacklogs() uint64 {
	if w == nil {
		return 0
	}
	return w.generatorQueueSaturation.Load()
}

// QuarantinedCount returns the number of offenders the world quarantined
// after their faults. It takes the lock of the log of faults, which no
// owner holds for long.
func (w *World) QuarantinedCount() int {
	if w == nil {
		return 0
	}
	w.faults.mu.Lock()
	defer w.faults.mu.Unlock()
	return len(w.faults.quarantined)
}

// Failed reports whether the world failed: it reported a fatal fault.
func (w *World) Failed() bool {
	return w != nil && w.faults.failed.Load()
}
