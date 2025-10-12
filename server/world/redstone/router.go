package redstone

import (
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
)

var (
	ErrUnknownChunk = errors.New("redstone: unknown chunk endpoint")
)

// SendState captures the result of routing a message.
type SendState uint8

const (
	SendDelivered SendState = iota
	SendCoalesced
	SendDropped
)

type SendResult struct {
	State SendState
	Err   error
}

type RouterConfig struct {
	Logger  *slog.Logger
	Metrics *Metrics
}

// Router maintains bounded inboxes for chunk workers and handles backpressure.
type Router struct {
	log       *slog.Logger
	metrics   *Metrics
	endpoints sync.Map // map[ChunkID]*endpoint
}

type endpoint struct {
	inbox chan Event

	hot atomic.Int32

	mu         sync.Mutex
	coalesced  map[EventKey]Event
	registered atomic.Bool
}

func NewRouter(cfg RouterConfig) *Router {
	return &Router{
		log:     cfg.Logger,
		metrics: cfg.Metrics,
	}
}

// Register attaches an inbox to the router for the given chunk.
func (r *Router) Register(id ChunkID, inbox chan Event) func() {
	ep := &endpoint{inbox: inbox, coalesced: make(map[EventKey]Event)}
	if _, loaded := r.endpoints.LoadOrStore(id, ep); loaded {
		// Replace existing endpoint to home the new worker, but keep behaviour deterministic.
		r.endpoints.Store(id, ep)
	}
	ep.registered.Store(true)
	return func() {
		r.endpoints.Delete(id)
	}
}

// Send attempts to deliver an event to the neighbour's inbox.
func (r *Router) Send(id ChunkID, ev Event) SendResult {
	raw, ok := r.endpoints.Load(id)
	if !ok {
		if r.metrics != nil {
			r.metrics.IncBackpressure(id)
		}
		return SendResult{State: SendDropped, Err: ErrUnknownChunk}
	}
	ep := raw.(*endpoint)
	select {
	case ep.inbox <- ev:
		return SendResult{State: SendDelivered}
	default:
		ep.mu.Lock()
		ep.hot.Store(1)
		ep.coalesced[ev.Key()] = ev
		ep.mu.Unlock()
		if r.metrics != nil {
			r.metrics.IncBackpressure(id)
		}
		return SendResult{State: SendCoalesced}
	}
}

// DrainCoalesced flushes coalesced events back to the caller in deterministic order.
func (r *Router) DrainCoalesced(id ChunkID) []Event {
	raw, ok := r.endpoints.Load(id)
	if !ok {
		return nil
	}
	ep := raw.(*endpoint)
	ep.mu.Lock()
	if len(ep.coalesced) == 0 {
		ep.mu.Unlock()
		return nil
	}
	events := make([]Event, 0, len(ep.coalesced))
	for _, ev := range ep.coalesced {
		events = append(events, ev)
	}
	clear(ep.coalesced)
	ep.mu.Unlock()
	sortEventsDeterministic(events)
	return events
}

// SnapshotHot returns the ids that have encountered backpressure since the last snapshot.
func (r *Router) SnapshotHot() []ChunkID {
	hot := make([]ChunkID, 0, 8)
	r.endpoints.Range(func(key, value any) bool {
		id := key.(ChunkID)
		ep := value.(*endpoint)
		if ep.hot.Swap(0) == 1 {
			hot = append(hot, id)
		}
		return true
	})
	return hot
}

// ClearHot resets the hot flag explicitly.
func (r *Router) ClearHot(id ChunkID) {
	if raw, ok := r.endpoints.Load(id); ok {
		raw.(*endpoint).hot.Store(0)
	}
}
