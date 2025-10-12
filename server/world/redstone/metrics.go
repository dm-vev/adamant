package redstone

import (
	"sync"
)

// Metrics tracks per-chunk counters for observability.
type Metrics struct {
	mu sync.Mutex

	ops          map[ChunkID]uint64
	backpressure map[ChunkID]uint64
	queue        map[ChunkID]int
	builds       map[ChunkID]uint64
}

// NewMetrics creates an empty metrics registry.
func NewMetrics() *Metrics {
	return &Metrics{
		ops:          make(map[ChunkID]uint64),
		backpressure: make(map[ChunkID]uint64),
		queue:        make(map[ChunkID]int),
		builds:       make(map[ChunkID]uint64),
	}
}

// AddOps increments the operations counter for a chunk.
func (m *Metrics) AddOps(id ChunkID, value uint64) {
	if m == nil || value == 0 {
		return
	}
	m.mu.Lock()
	m.ops[id] += value
	m.mu.Unlock()
}

// IncBackpressure increments the backpressure counter for a chunk.
func (m *Metrics) IncBackpressure(id ChunkID) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.backpressure[id]++
	m.mu.Unlock()
}

// SetQueueSize stores the current queue size gauge for a chunk.
func (m *Metrics) SetQueueSize(id ChunkID, size int) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.queue[id] = size
	m.mu.Unlock()
}

// IncBuilds increments the graph build counter for a chunk.
func (m *Metrics) IncBuilds(id ChunkID) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.builds[id]++
	m.mu.Unlock()
}
