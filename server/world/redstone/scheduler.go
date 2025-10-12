package redstone

import (
	"context"
	"log/slog"
	"sort"
)

type SchedulerConfig struct {
	Logger           *slog.Logger
	Router           *Router
	InboxSize        int
	BudgetPerTick    int
	ProcessorFactory ProcessorFactory
	Metrics          *Metrics
}

// Scheduler ticks chunk workers in a deterministic order.
type Scheduler struct {
	log *slog.Logger

	router *Router

	inboxSize     int
	budgetPerTick int

	processorFactory ProcessorFactory

	chunks map[ChunkID]*ChunkWorker
	order  []ChunkID
	dirty  bool

	saturation map[ChunkID]int
	penalty    map[ChunkID]int

	metrics *Metrics
}

func NewScheduler(cfg SchedulerConfig) *Scheduler {
	if cfg.Router == nil {
		panic("redstone: scheduler requires router")
	}
	if cfg.ProcessorFactory == nil {
		cfg.ProcessorFactory = ProcessorFactoryFunc(func(id ChunkID) Processor { return NopProcessor{} })
	}
	if cfg.InboxSize <= 0 {
		cfg.InboxSize = 4096
	}
	if cfg.BudgetPerTick <= 0 {
		cfg.BudgetPerTick = 8192
	}
	return &Scheduler{
		log:              cfg.Logger,
		router:           cfg.Router,
		inboxSize:        cfg.InboxSize,
		budgetPerTick:    cfg.BudgetPerTick,
		processorFactory: cfg.ProcessorFactory,
		chunks:           make(map[ChunkID]*ChunkWorker),
		order:            make([]ChunkID, 0, 16),
		saturation:       make(map[ChunkID]int),
		penalty:          make(map[ChunkID]int),
		metrics:          cfg.Metrics,
	}
}

// RegisterChunk installs a worker for the chunk id.
func (s *Scheduler) RegisterChunk(id ChunkID, graph Graph) {
	if _, ok := s.chunks[id]; ok {
		return
	}
	worker := NewChunkWorker(WorkerConfig{
		Logger:    s.log,
		Router:    s.router,
		Chunk:     id,
		InboxSize: s.inboxSize,
		Processor: s.processorFactory.New(id),
	})
	worker.UpdateGraph(graph)
	s.chunks[id] = worker
	s.order = append(s.order, id)
	s.saturation[id] = 0
	s.penalty[id] = 0
	if s.metrics != nil {
		s.metrics.IncBuilds(id)
		s.metrics.SetQueueSize(id, 0)
	}
	s.dirty = true
}

// UnregisterChunk stops and removes the worker for the chunk.
func (s *Scheduler) UnregisterChunk(id ChunkID) {
	worker, ok := s.chunks[id]
	if !ok {
		return
	}
	worker.Stop()
	delete(s.chunks, id)
	delete(s.saturation, id)
	delete(s.penalty, id)
	if s.metrics != nil {
		s.metrics.SetQueueSize(id, 0)
	}
	s.dirty = true
}

// UpdateGraph replaces the worker graph when the chunk changes.
func (s *Scheduler) UpdateGraph(id ChunkID, graph Graph) {
	if worker, ok := s.chunks[id]; ok {
		worker.UpdateGraph(graph)
		if s.metrics != nil {
			s.metrics.IncBuilds(id)
		}
	}
}

// QueueLocal schedules a local event directly inside the worker.
func (s *Scheduler) QueueLocal(id ChunkID, ev Event) {
	if worker, ok := s.chunks[id]; ok {
		worker.EnqueueLocal(ev)
	}
}

// Step advances all registered workers for the tick, respecting determinism.
func (s *Scheduler) Step(ctx context.Context, tick int64) {
	if len(s.chunks) == 0 {
		return
	}
	if s.dirty {
		s.rebuildOrder()
	}
	hot := make(map[ChunkID]struct{}, len(s.chunks)/4+1)
	for _, id := range s.router.SnapshotHot() {
		hot[id] = struct{}{}
	}
	for _, id := range s.order {
		worker := s.chunks[id]
		if worker == nil {
			continue
		}
		budget := s.budgetPerTick
		if _, isHot := hot[id]; isHot {
			budget += budget / 2
		}
		if penalty := s.penalty[id]; penalty > 0 {
			budget = max(1, budget>>penalty)
		}
		res := worker.Step(ctx, StepRequest{
			Tick:   tick,
			Budget: budget,
			Ctx:    ctx,
		})
		if res.Err != nil && s.log != nil {
			s.log.Error("redstone step failed", "chunkX", id.X, "chunkZ", id.Z, "err", res.Err)
			continue
		}
		if res.Hot {
			hot[id] = struct{}{}
		} else {
			delete(hot, id)
		}
		if s.metrics != nil {
			s.metrics.AddOps(id, uint64(res.Ops))
			s.metrics.SetQueueSize(id, res.QueueLen)
		}
		s.updateWatchdog(id, res.Ops, budget)
	}
}

func (s *Scheduler) rebuildOrder() {
	s.order = s.order[:0]
	for id := range s.chunks {
		s.order = append(s.order, id)
	}
	sort.Slice(s.order, func(i, j int) bool {
		return s.order[i].Morton() < s.order[j].Morton()
	})
	s.dirty = false
}

func (s *Scheduler) updateWatchdog(id ChunkID, ops, budget int) {
	if budget <= 0 {
		return
	}
	if ops >= budget {
		s.saturation[id]++
		if s.saturation[id] >= 3 {
			if s.penalty[id] < 3 {
				s.penalty[id]++
			}
			s.saturation[id] = 0
		}
		return
	}
	s.saturation[id] = 0
	if s.penalty[id] > 0 {
		s.penalty[id]--
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
