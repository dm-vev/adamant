package redstone

import (
	"context"
	"log/slog"
	"sync"

	"github.com/df-mc/dragonfly/server/block/cube"
)

type WorkerConfig struct {
	Logger    *slog.Logger
	Router    *Router
	Chunk     ChunkID
	InboxSize int
	Processor Processor
}

// ChunkWorker owns the redstone execution state for a single chunk.
type ChunkWorker struct {
	id        ChunkID
	log       *slog.Logger
	router    *Router
	processor Processor

	currentTick int64

	graph Graph

	inbox chan Event

	localQueue []Event

	metrics struct {
		coalesced int
	}

	cmdCh    chan workerCommand
	stopOnce sync.Once
	stopFn   func()
}

type eventDedupeKey struct {
	Pos   cube.Pos
	Tick  int64
	Power uint8
	Kind  EventKind
}

func NewChunkWorker(cfg WorkerConfig) *ChunkWorker {
	if cfg.InboxSize <= 0 {
		cfg.InboxSize = 4096
	}
	if cfg.Processor == nil {
		cfg.Processor = NopProcessor{}
	}
	w := &ChunkWorker{
		id:        cfg.Chunk,
		log:       cfg.Logger,
		router:    cfg.Router,
		processor: cfg.Processor,
		inbox:     make(chan Event, cfg.InboxSize),
		cmdCh:     make(chan workerCommand, 8),
	}
	w.stopFn = cfg.Router.Register(cfg.Chunk, w.inbox)
	go w.loop()
	return w
}

func (w *ChunkWorker) loop() {
	for cmd := range w.cmdCh {
		if !cmd.execute(w) {
			break
		}
	}
	if w.stopFn != nil {
		w.stopFn()
	}
}

// StepRequest instructs the worker to advance its local simulation.
type StepRequest struct {
	Tick   int64
	Budget int
	Ctx    context.Context
}

// StepResult summarises the work performed in a Step.
type StepResult struct {
	Ops      int
	Hot      bool
	Coalesce int
	QueueLen int
	Err      error
}

func (w *ChunkWorker) Step(ctx context.Context, req StepRequest) StepResult {
	resp := make(chan StepResult, 1)
	select {
	case w.cmdCh <- stepCommand{req: req, resp: resp}:
	case <-ctx.Done():
		return StepResult{Err: ctx.Err()}
	}
	select {
	case res := <-resp:
		return res
	case <-ctx.Done():
		return StepResult{Err: ctx.Err()}
	}
}

func (w *ChunkWorker) EnqueueLocal(ev Event) {
	done := make(chan struct{})
	w.cmdCh <- enqueueCommand{event: ev, done: done}
	<-done
}

func (w *ChunkWorker) UpdateGraph(graph Graph) {
	done := make(chan struct{})
	w.cmdCh <- updateGraphCommand{graph: graph, done: done}
	<-done
}

func (w *ChunkWorker) Stop() {
	w.stopOnce.Do(func() {
		done := make(chan struct{})
		w.cmdCh <- stopCommand{done: done}
		<-done
		close(w.cmdCh)
	})
}

func (w *ChunkWorker) runStep(req StepRequest) StepResult {
	w.currentTick = req.Tick
	w.metrics.coalesced = 0

	drained := w.router.DrainCoalesced(w.id)
	if len(drained) > 0 {
		w.metrics.coalesced += len(drained)
		w.localQueue = append(w.localQueue, drained...)
	}

	for {
		select {
		case ev := <-w.inbox:
			w.localQueue = append(w.localQueue, ev)
		default:
			goto inboxDrained
		}
	}
inboxDrained:
	ops := 0
	outputs := make([]Event, 0, 4)
	em := emitter{worker: w, outputs: &outputs}
	seen := make(map[eventDedupeKey]struct{}, len(w.localQueue))
	future := make([]Event, 0, len(w.localQueue))
	overflow := make([]Event, 0)
	for len(w.localQueue) > 0 {
		ev := w.localQueue[0]
		w.localQueue = w.localQueue[1:]
		if ev.Tick > w.currentTick {
			future = append(future, ev)
			continue
		}
		if req.Budget <= 0 {
			overflow = append(overflow, ev)
			continue
		}
		if ev.Tick < w.currentTick {
			ev.Tick = w.currentTick
		}
		key := eventDedupeKey{
			Pos:   ev.Pos,
			Tick:  ev.Tick,
			Power: ev.Power,
			Kind:  ev.Kind,
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		w.processor.HandleEvent(w.id, &w.graph, ev, em)
		req.Budget--
		ops++
	}
	w.localQueue = append(overflow, future...)

	queueLen := len(w.localQueue) + len(w.inbox)

	hot := len(w.localQueue) > 0 || len(w.inbox) > 0
	if !hot {
		w.router.ClearHot(w.id)
	}
	return StepResult{
		Ops:      ops,
		Hot:      hot,
		Coalesce: w.metrics.coalesced,
		QueueLen: queueLen,
		Outputs:  outputs,
	}
}

type workerCommand interface {
	execute(*ChunkWorker) bool
}

type stepCommand struct {
	req  StepRequest
	resp chan StepResult
}

func (cmd stepCommand) execute(w *ChunkWorker) bool {
	res := w.runStep(cmd.req)
	cmd.resp <- res
	return true
}

type enqueueCommand struct {
	event Event
	done  chan struct{}
}

func (cmd enqueueCommand) execute(w *ChunkWorker) bool {
	for i := len(w.localQueue) - 1; i >= 0; i-- {
		if w.localQueue[i].Pos == cmd.event.Pos && w.localQueue[i].Tick == cmd.event.Tick && w.localQueue[i].Kind == cmd.event.Kind {
			w.localQueue[i] = cmd.event
			close(cmd.done)
			return true
		}
	}
	w.localQueue = append(w.localQueue, cmd.event)
	close(cmd.done)
	return true
}

type updateGraphCommand struct {
	graph Graph
	done  chan struct{}
}

func (cmd updateGraphCommand) execute(w *ChunkWorker) bool {
	w.graph = cmd.graph.Clone()
	close(cmd.done)
	return true
}

type stopCommand struct {
	done chan struct{}
}

func (cmd stopCommand) execute(w *ChunkWorker) bool {
	close(cmd.done)
	return false
}

type emitter struct {
	worker  *ChunkWorker
	outputs *[]Event
}

func (e emitter) Local(ev Event) {
	if ev.Tick < e.worker.currentTick {
		ev.Tick = e.worker.currentTick
	}
	e.worker.localQueue = append(e.worker.localQueue, ev)
}

func (e emitter) Remote(id ChunkID, ev Event) {
	if ev.Tick <= e.worker.currentTick {
		ev.Tick = e.worker.currentTick + 1
	}
	ev.Node = 0
	ev.Meta = 0
	if id == e.worker.id {
		e.Local(ev)
		return
	}
	if res := e.worker.router.Send(id, ev); res.Err != nil && e.worker.log != nil {
		e.worker.log.Warn("redstone router dropped event", "chunkX", id.X, "chunkZ", id.Z, "err", res.Err)
	}
}

func (e emitter) Output(ev Event) {
	if ev.Tick < e.worker.currentTick {
		ev.Tick = e.worker.currentTick
	}
	if ev.Kind == EventUnknown {
		ev.Kind = EventOutput
	}
	if e.outputs != nil {
		*e.outputs = append(*e.outputs, ev)
	}
}
