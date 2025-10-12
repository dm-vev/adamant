package redstone

import "context"

// System ties the scheduler and router together for use by the world.
type System struct {
	router    *Router
	scheduler *Scheduler
}

func (s *System) Enabled() bool {
	return s != nil && s.scheduler != nil
}

func (s *System) RegisterChunk(id ChunkID, graph Graph) {
	if s == nil || s.scheduler == nil {
		return
	}
	s.scheduler.RegisterChunk(id, graph)
}

func (s *System) UnregisterChunk(id ChunkID) {
	if s == nil || s.scheduler == nil {
		return
	}
	s.scheduler.UnregisterChunk(id)
}

func (s *System) UpdateGraph(id ChunkID, graph Graph) {
	if s == nil || s.scheduler == nil {
		return
	}
	s.scheduler.UpdateGraph(id, graph)
}

func (s *System) QueueLocal(id ChunkID, ev Event) {
	if s == nil || s.scheduler == nil {
		return
	}
	s.scheduler.QueueLocal(id, ev)
}

func (s *System) Step(ctx context.Context, tick int64) {
	if s == nil || s.scheduler == nil {
		return
	}
	s.scheduler.Step(ctx, tick)
}

func (s *System) Metrics() *Metrics {
	if s == nil {
		return nil
	}
	return s.metrics
}
