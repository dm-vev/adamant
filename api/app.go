package api

import (
	"iter"
	"slices"
	"sync"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/event"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

// Plugin defines an external module lifecycle bound to an App.
type Plugin interface {
	Enable(app *App)
	Disable(app *App)
}

// FuncPlugin adapts Enable and Disable callbacks into a Plugin.
type FuncPlugin struct {
	OnEnable  func(app *App)
	OnDisable func(app *App)
}

// Enable invokes OnEnable when set.
func (p FuncPlugin) Enable(app *App) {
	if p.OnEnable != nil {
		p.OnEnable(app)
	}
}

// Disable invokes OnDisable when set.
func (p FuncPlugin) Disable(app *App) {
	if p.OnDisable != nil {
		p.OnDisable(app)
	}
}

// App is the primary SDK facade wrapping a *server.Server and managing plugin
// lifecycles, world access, event buses, and transactions.
type App struct {
	srv     *server.Server
	mu      sync.Mutex
	plugins []pluginEntry
}

type pluginEntry struct {
	plugin  Plugin
	enabled bool
}

// New creates an App facade around srv.
func New(srv *server.Server) *App {
	return &App{srv: srv}
}

// Use registers and enables each non-nil plugin on the App in order and returns
// the App for chaining.
func (a *App) Use(plugins ...Plugin) *App {
	if a == nil {
		return nil
	}
	for _, p := range plugins {
		if p == nil {
			continue
		}
		a.mu.Lock()
		idx := len(a.plugins)
		a.plugins = append(a.plugins, pluginEntry{plugin: p})
		a.mu.Unlock()

		p.Enable(a)

		a.mu.Lock()
		a.plugins[idx].enabled = true
		a.mu.Unlock()
	}
	return a
}

// Disable disables all currently enabled plugins in reverse registration order.
// Calling Disable multiple times is safe and idempotent.
func (a *App) Disable() {
	if a == nil {
		return
	}
	a.mu.Lock()
	toDisable := make([]Plugin, 0, len(a.plugins))
	for i := len(a.plugins) - 1; i >= 0; i-- {
		if a.plugins[i].enabled {
			a.plugins[i].enabled = false
			toDisable = append(toDisable, a.plugins[i].plugin)
		}
	}
	a.mu.Unlock()

	for _, p := range toDisable {
		p.Disable(a)
	}
}

// Plugins returns a snapshot of plugins registered on the App.
func (a *App) Plugins() []Plugin {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]Plugin, len(a.plugins))
	for i, entry := range a.plugins {
		out[i] = entry.plugin
	}
	return slices.Clip(out)
}

// Server returns the underlying *server.Server.
func (a *App) Server() *server.Server {
	if a == nil {
		return nil
	}
	return a.srv
}

// Events returns the server-wide prioritized event bus, or nil if unavailable.
func (a *App) Events() *event.Bus {
	if a == nil || a.srv == nil {
		return nil
	}
	return a.srv.Events()
}

// World returns the server's default world, or nil if unavailable.
func (a *App) World() *world.World {
	if a == nil || a.srv == nil {
		return nil
	}
	return a.srv.World()
}

// Nether returns the server's nether world, or nil if unavailable.
func (a *App) Nether() *world.World {
	if a == nil || a.srv == nil {
		return nil
	}
	return a.srv.Nether()
}

// End returns the server's end world, or nil if unavailable.
func (a *App) End() *world.World {
	if a == nil || a.srv == nil {
		return nil
	}
	return a.srv.End()
}

// Players yields every online player across worlds, each within its world's
// transaction. Do not call from inside a world owner transaction; use PlayersIn
// or Online instead.
func (a *App) Players() iter.Seq[*player.Player] {
	if a == nil || a.srv == nil {
		return func(func(*player.Player) bool) {}
	}
	return a.srv.Players(nil)
}

// PlayersIn yields the online players in tx's world within tx.
func (a *App) PlayersIn(tx *world.Tx) iter.Seq[*player.Player] {
	if a == nil || a.srv == nil {
		return func(func(*player.Player) bool) {}
	}
	return a.srv.Players(tx)
}

// Online returns a lock-free snapshot of online players across all worlds.
func (a *App) Online() []server.OnlinePlayer {
	if a == nil || a.srv == nil {
		return nil
	}
	return a.srv.Online()
}

// ExecuteWorld runs fn synchronously inside a transaction on w (defaulting to
// a.World() when w is nil). Do not call from within w's owner goroutine.
func (a *App) ExecuteWorld(w *world.World, fn func(tx *world.Tx)) {
	if fn == nil {
		return
	}
	if w == nil && a != nil && a.srv != nil {
		w = a.srv.World()
	}
	if w == nil {
		return
	}
	<-w.Exec(fn)
}
