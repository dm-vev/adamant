package redstone

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
)

func TestSchedulerRoutesCrossChunkEvents(t *testing.T) {
	ctx := context.Background()
	router := NewRouter(RouterConfig{})
	factory := &recordingFactory{
		handled:     make(map[ChunkID][]Event),
		forwardFrom: ChunkID{X: 0, Z: 0},
		forwardTo:   ChunkID{X: 1, Z: 0},
	}
	sched := NewScheduler(SchedulerConfig{
		Router:           router,
		InboxSize:        4,
		BudgetPerTick:    8,
		ProcessorFactory: factory,
	})

	chunkA := ChunkID{X: 0, Z: 0}
	chunkB := ChunkID{X: 1, Z: 0}

	sched.RegisterChunk(chunkA, Graph{})
	sched.RegisterChunk(chunkB, Graph{})

	sched.QueueLocal(chunkA, Event{Kind: EventSignalRise, Tick: 1})

	sched.Step(ctx, 1)

	if got := len(factory.Events(chunkA)); got != 1 {
		t.Fatalf("expected chunk A to process 1 event, got %d", got)
	}
	if got := len(factory.Events(chunkB)); got != 0 {
		t.Fatalf("expected chunk B to have no events on tick 1, got %d", got)
	}

	sched.Step(ctx, 2)

	eventsB := factory.Events(chunkB)
	if len(eventsB) != 1 {
		t.Fatalf("expected chunk B to process forwarded event on tick 2, got %d", len(eventsB))
	}
	if eventsB[0].Tick < 2 {
		t.Fatalf("expected forwarded event tick >= 2, got %d", eventsB[0].Tick)
	}
}

func TestRouterSendUnknownChunk(t *testing.T) {
	router := NewRouter(RouterConfig{})
	res := router.Send(ChunkID{X: 42, Z: 42}, Event{})
	if res.State != SendDropped {
		t.Fatalf("expected dropped state for unknown chunk, got %v", res.State)
	}
	if !errors.Is(res.Err, ErrUnknownChunk) {
		t.Fatalf("expected ErrUnknownChunk, got %v", res.Err)
	}
}

func TestWorkerDeduplicatesEvents(t *testing.T) {
	router := NewRouter(RouterConfig{})
	proc := &collectingProcessor{}
	worker := NewChunkWorker(WorkerConfig{
		Router:    router,
		Chunk:     ChunkID{X: 0, Z: 0},
		InboxSize: 8,
		Processor: proc,
	})
	t.Cleanup(worker.Stop)

	pos := cube.Pos{0, 64, 0}
	dup := Event{Pos: pos, Power: 15, Tick: 10}

	worker.EnqueueLocal(dup)
	worker.EnqueueLocal(dup)

	res := worker.Step(context.Background(), StepRequest{
		Tick:   10,
		Budget: 4,
	})
	if res.Ops != 1 {
		t.Fatalf("expected exactly one operation, got %d", res.Ops)
	}

	events := proc.Events()
	if len(events) != 1 {
		t.Fatalf("expected processor to receive one event, got %d", len(events))
	}
	if events[0].Tick != 10 {
		t.Fatalf("expected processed event tick to be 10, got %d", events[0].Tick)
	}
}

func TestSchedulerWatchdogPenalisesChunk(t *testing.T) {
	router := NewRouter(RouterConfig{})
	sched := NewScheduler(SchedulerConfig{
		Router:           router,
		InboxSize:        8,
		BudgetPerTick:    4,
		ProcessorFactory: ProcessorFactoryFunc(func(ChunkID) Processor { return NopProcessor{} }),
	})

	id := ChunkID{X: 3, Z: 7}
	sched.RegisterChunk(id, Graph{})

	for i := 0; i < 16; i++ {
		sched.QueueLocal(id, Event{
			Pos:   cube.Pos{int(i), 64, 0},
			Power: uint8(i),
			Tick:  1,
		})
	}

	penalised := false
	for tick := int64(1); tick <= 6; tick++ {
		sched.Step(context.Background(), tick)
		if sched.penalty[id] > 0 {
			penalised = true
			break
		}
	}

	if !penalised {
		t.Fatalf("expected watchdog to penalise chunk, got penalty 0")
	}
}

func TestGraphProcessorSourceWireLamp(t *testing.T) {
	router := NewRouter(RouterConfig{})
	worker := NewChunkWorker(WorkerConfig{
		Router:    router,
		Chunk:     ChunkID{X: 0, Z: 0},
		InboxSize: 16,
		Processor: NewGraphProcessor(),
	})
	t.Cleanup(worker.Stop)

	graph := Graph{
		Gen: 1,
		Palette: []Node{
			{ID: 0, Kind: NodePowerSource, Data: sourceLever, Pos: cube.Pos{0, 64, 0}},
			{ID: 1, Kind: NodeWire, Pos: cube.Pos{1, 64, 0}},
			{ID: 2, Kind: NodeLamp, Pos: cube.Pos{2, 64, 0}},
		},
		Offsets:   []uint32{0, 1, 2, 2},
		Adjacency: []NodeID{1, 2},
	}
	graph.Reindex()
	worker.UpdateGraph(graph)

	worker.EnqueueLocal(Event{
		Kind:  EventSignalRise,
		Pos:   graph.Palette[0].Pos,
		Power: 15,
		Tick:  1,
		Node:  graph.Palette[0].ID,
	})

	res := worker.Step(context.Background(), StepRequest{
		Tick:   1,
		Budget: 16,
	})

	if len(worker.graph.States) < 3 {
		t.Fatalf("expected 3 node states, got %d", len(worker.graph.States))
	}
	if power := worker.graph.States[0].Power; power != 15 {
		t.Fatalf("expected source power 15, got %d", power)
	}
	if power := worker.graph.States[1].Power; power != 14 {
		t.Fatalf("expected wire power 14, got %d", power)
	}
	if power := worker.graph.States[2].Power; power != 14 {
		t.Fatalf("expected lamp power 14, got %d", power)
	}
	if len(res.Outputs) != 1 {
		t.Fatalf("expected one output event, got %d", len(res.Outputs))
	}
	output := res.Outputs[0]
	if output.Kind != EventOutput {
		t.Fatalf("expected output event kind EventOutput, got %v", output.Kind)
	}
	if output.Meta != 1 {
		t.Fatalf("expected lamp to be active, meta=1, got %d", output.Meta)
	}
}

func TestButtonAutoRelease(t *testing.T) {
	router := NewRouter(RouterConfig{})
	worker := NewChunkWorker(WorkerConfig{
		Router:    router,
		Chunk:     ChunkID{X: 0, Z: 0},
		InboxSize: 16,
		Processor: NewGraphProcessor(),
	})
	t.Cleanup(worker.Stop)

	graph := Graph{
		Gen: 1,
		Palette: []Node{
			{ID: 0, Kind: NodePowerSource, Data: sourceButton, Pos: cube.Pos{0, 64, 0}},
			{ID: 1, Kind: NodeLamp, Pos: cube.Pos{1, 64, 0}},
		},
		Offsets:   []uint32{0, 1, 1},
		Adjacency: []NodeID{1},
	}
	graph.Reindex()
	worker.UpdateGraph(graph)

	worker.EnqueueLocal(Event{
		Kind:  EventSignalRise,
		Pos:   graph.Palette[0].Pos,
		Power: 15,
		Tick:  1,
		Node:  graph.Palette[0].ID,
		Meta:  2,
	})

	res1 := worker.Step(context.Background(), StepRequest{
		Tick:   1,
		Budget: 16,
	})
	if len(res1.Outputs) == 0 {
		t.Fatalf("expected output event when button pressed")
	}
	if res1.Outputs[0].Meta != 1 {
		t.Fatalf("expected lamp output meta=1 on press, got %d", res1.Outputs[0].Meta)
	}

	worker.Step(context.Background(), StepRequest{
		Tick:   2,
		Budget: 16,
	})

	res3 := worker.Step(context.Background(), StepRequest{
		Tick:   3,
		Budget: 16,
	})
	if res3.Outputs == nil || len(res3.Outputs) == 0 {
		t.Fatalf("expected output event when button released")
	}
	if res3.Outputs[0].Meta != 0 {
		t.Fatalf("expected lamp output meta=0 on release, got %d", res3.Outputs[0].Meta)
	}
	if power := worker.graph.States[0].Power; power != 0 {
		t.Fatalf("expected button power 0 after release, got %d", power)
	}
}

func TestTorchInversion(t *testing.T) {
	router := NewRouter(RouterConfig{})
	worker := NewChunkWorker(WorkerConfig{
		Router:    router,
		Chunk:     ChunkID{X: 0, Z: 0},
		InboxSize: 16,
		Processor: NewGraphProcessor(),
	})
	t.Cleanup(worker.Stop)

	graph := Graph{
		Gen: 1,
		Palette: []Node{
			{ID: 0, Kind: NodePowerSource, Data: sourceTorch, Pos: cube.Pos{0, 64, 0}},
			{ID: 1, Kind: NodeWire, Pos: cube.Pos{1, 64, 0}},
		},
		Offsets:   []uint32{0, 1, 1},
		Adjacency: []NodeID{1},
	}
	graph.Reindex()
	worker.UpdateGraph(graph)

	worker.EnqueueLocal(Event{
		Kind:  EventSignalRise,
		Pos:   graph.Palette[0].Pos,
		Power: 15,
		Tick:  1,
		Node:  graph.Palette[0].ID,
	})
	worker.Step(context.Background(), StepRequest{
		Tick:   1,
		Budget: 16,
	})

	if power := worker.graph.States[0].Power; power != 15 {
		t.Fatalf("expected torch power 15, got %d", power)
	}

	worker.EnqueueLocal(Event{
		Kind:  EventPowerChange,
		Pos:   graph.Palette[0].Pos,
		Power: 15,
		Tick:  2,
		Node:  graph.Palette[0].ID,
	})
	worker.Step(context.Background(), StepRequest{
		Tick:   2,
		Budget: 16,
	})
	if power := worker.graph.States[0].Power; power != 0 {
		t.Fatalf("expected torch to invert off when powered, got %d", power)
	}
	if power := worker.graph.States[1].Power; power != 0 {
		t.Fatalf("expected connected wire to be off, got %d", power)
	}

	worker.EnqueueLocal(Event{
		Kind:  EventPowerChange,
		Pos:   graph.Palette[0].Pos,
		Power: 0,
		Tick:  3,
		Node:  graph.Palette[0].ID,
	})
	worker.Step(context.Background(), StepRequest{
		Tick:   3,
		Budget: 16,
	})
	if power := worker.graph.States[0].Power; power != 15 {
		t.Fatalf("expected torch to relight, got %d", power)
	}
	if power := worker.graph.States[1].Power; power != 14 {
		t.Fatalf("expected wire to receive power 14, got %d", power)
	}
}

type recordingFactory struct {
	mu sync.Mutex

	handled map[ChunkID][]Event

	forwardFrom ChunkID
	forwardTo   ChunkID
	forwarded   bool
}

func (f *recordingFactory) New(id ChunkID) Processor {
	return &recordingProcessor{id: id, factory: f}
}

func (f *recordingFactory) record(id ChunkID, ev Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handled[id] = append(f.handled[id], ev)
}

func (f *recordingFactory) Events(id ChunkID) []Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Event, len(f.handled[id]))
	copy(out, f.handled[id])
	return out
}

type recordingProcessor struct {
	id      ChunkID
	factory *recordingFactory
}

func (p *recordingProcessor) HandleEvent(chunk ChunkID, _ *Graph, ev Event, emit Emitter) {
	p.factory.record(chunk, ev)
	if chunk == p.factory.forwardFrom {
		p.factory.mu.Lock()
		shouldForward := !p.factory.forwarded
		if shouldForward {
			p.factory.forwarded = true
		}
		p.factory.mu.Unlock()
		if shouldForward {
			emit.Remote(p.factory.forwardTo, Event{
				Kind: EventSignalFall,
				Tick: ev.Tick,
			})
		}
	}
}

type collectingProcessor struct {
	mu     sync.Mutex
	events []Event
}

func (p *collectingProcessor) HandleEvent(_ ChunkID, _ *Graph, ev Event, _ Emitter) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, ev)
}

func (p *collectingProcessor) Events() []Event {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]Event, len(p.events))
	copy(out, p.events)
	return out
}
