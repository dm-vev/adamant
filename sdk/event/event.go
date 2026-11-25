// Package event provides prioritized, typed event subscriptions with automatic
// plugin-context cleanup on disable.
package event

import (
	"github.com/df-mc/dragonfly/sdk/plugin"
	core "github.com/df-mc/dragonfly/server/event"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

// Priority orders event listeners from earliest (Lowest) to latest (Monitor).
type Priority = core.Priority

const (
	Lowest  = core.PriorityLowest
	Low     = core.PriorityLow
	Normal  = core.PriorityNormal
	High    = core.PriorityHigh
	Highest = core.PriorityHighest
	Monitor = core.PriorityMonitor
)

// Bus dispatches typed events to registered listeners in priority order.
type Bus = core.Bus

// Subscription represents a registered listener that can be cancelled.
type Subscription = core.Subscription

// Cancellable may be embedded into custom event structs to support cancellation.
type Cancellable = core.Cancellable

// NewBus creates a standalone typed event bus.
func NewBus() *Bus { return core.NewBus() }

// On registers fn to listen for *E at priority p on bus.
func On[E any](bus *Bus, p Priority, fn func(*E)) Subscription {
	return core.On(bus, p, fn)
}

// Emit dispatches ev to all listeners registered for *E on bus in priority order.
// Returns true if ev was not cancelled (or is not cancellable).
func Emit[E any](bus *Bus, ev *E) bool { return core.Emit(bus, ev) }

// HasListeners reports whether bus has at least one listener registered for *E.
func HasListeners[E any](bus *Bus) bool { return core.HasListeners[E](bus) }

// OnServer registers fn on the server-wide event bus. When ctx is non-nil, the
// subscription is automatically unsubscribed when the plugin disables.
func OnServer[E any](ctx *plugin.Context, p Priority, fn func(*E)) Subscription {
	if ctx == nil {
		return Subscription{}
	}
	sub := core.On(ctx.Events(), p, fn)
	ctx.OnDisable(sub.Unsubscribe)
	return sub
}

// OnWorld registers fn on world w's event bus. When ctx is non-nil, the
// subscription is automatically unsubscribed when the plugin disables.
func OnWorld[E any](ctx *plugin.Context, w *world.World, p Priority, fn func(*E)) Subscription {
	if w == nil {
		return Subscription{}
	}
	sub := core.On(w.Events(), p, fn)
	if ctx != nil {
		ctx.OnDisable(sub.Unsubscribe)
	}
	return sub
}

// OnPlayer registers fn on player p's event bus. When ctx is non-nil, the
// subscription is automatically unsubscribed when the plugin disables.
func OnPlayer[E any](ctx *plugin.Context, p *player.Player, prio Priority, fn func(*E)) Subscription {
	if p == nil {
		return Subscription{}
	}
	sub := core.On(p.Events(), prio, fn)
	if ctx != nil {
		ctx.OnDisable(sub.Unsubscribe)
	}
	return sub
}
