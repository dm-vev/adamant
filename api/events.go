package api

import (
	"github.com/df-mc/dragonfly/server/event"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

// Priority orders event listeners from earliest (PriorityLowest) to latest
// (PriorityMonitor).
type Priority = event.Priority

const (
	PriorityLowest  = event.PriorityLowest
	PriorityLow     = event.PriorityLow
	PriorityNormal  = event.PriorityNormal
	PriorityHigh    = event.PriorityHigh
	PriorityHighest = event.PriorityHighest
	PriorityMonitor = event.PriorityMonitor
)

// EventBus dispatches typed events to registered listeners in priority order.
type EventBus = event.Bus

// Subscription represents a registered event listener on a bus.
type Subscription = event.Subscription

// Cancellable may be embedded into custom event structs to support cancellation.
type Cancellable = event.Cancellable

// NewEventBus creates a new standalone typed event bus.
func NewEventBus() *EventBus {
	return event.NewBus()
}

// On registers fn to listen for events of type *E at priority p on bus.
func On[E any](bus *EventBus, p Priority, fn func(*E)) Subscription {
	return event.On(bus, p, fn)
}

// Emit dispatches ev to all listeners registered for *E on bus in priority order.
// Returns true if ev was not cancelled (or is not cancellable).
func Emit[E any](bus *EventBus, ev *E) bool {
	return event.Emit(bus, ev)
}

// HasListeners reports whether bus has at least one listener registered for *E.
func HasListeners[E any](bus *EventBus) bool {
	return event.HasListeners[E](bus)
}

// OnServer registers fn on app's server-wide event bus at priority p.
func OnServer[E any](app *App, p Priority, fn func(*E)) Subscription {
	if app == nil {
		return Subscription{}
	}
	return event.On(app.Events(), p, fn)
}

// OnWorld registers fn on world w's event bus at priority p.
func OnWorld[E any](w *world.World, p Priority, fn func(*E)) Subscription {
	if w == nil {
		return Subscription{}
	}
	return event.On(w.Events(), p, fn)
}

// OnPlayer registers fn on player p's per-player event bus at priority prio.
func OnPlayer[E any](p *player.Player, prio Priority, fn func(*E)) Subscription {
	if p == nil {
		return Subscription{}
	}
	return event.On(p.Events(), prio, fn)
}
