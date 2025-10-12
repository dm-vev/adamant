package redstone

// Processor consumes events for a chunk and can emit follow-up events.
type Processor interface {
	HandleEvent(chunk ChunkID, graph *Graph, ev Event, emit Emitter)
}

// ProcessorFactory instantiates processors per chunk.
type ProcessorFactory interface {
	New(chunk ChunkID) Processor
}

// ProcessorFactoryFunc adapts a function into a ProcessorFactory.
type ProcessorFactoryFunc func(chunk ChunkID) Processor

func (f ProcessorFactoryFunc) New(chunk ChunkID) Processor {
	return f(chunk)
}

// NopProcessor drops all events and serves as the default implementation.
type NopProcessor struct{}

func (NopProcessor) HandleEvent(_ ChunkID, _ *Graph, _ Event, _ Emitter) {}

// Emitter allows a processor to enqueue new events in a chunk-safe way.
type Emitter interface {
	Local(Event)
	Remote(ChunkID, Event)
	Output(Event)
}
