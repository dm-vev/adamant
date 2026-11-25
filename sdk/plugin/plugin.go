// Package plugin provides the plugin lifecycle, scoped plugin Context
// (logging, atomic TOML/JSON storage, and automatic resource cleanup on
// disable), and a typed inter-plugin service registry for Adamant's SDK.
package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/event"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/pelletier/go-toml"
)

// Plugin defines a modular plugin lifecycle managed by an App.
type Plugin interface {
	Name() string
	Enable(ctx *Context)
	Disable(ctx *Context)
}

type funcPlugin struct {
	name      string
	onEnable  func(*Context)
	onDisable func(*Context)
}

func (p funcPlugin) Name() string { return p.name }

func (p funcPlugin) Enable(ctx *Context) {
	if p.onEnable != nil {
		p.onEnable(ctx)
	}
}

func (p funcPlugin) Disable(ctx *Context) {
	if p.onDisable != nil {
		p.onDisable(ctx)
	}
}

// Func adapts enable and disable callbacks into a named Plugin.
func Func(name string, onEnable, onDisable func(*Context)) Plugin {
	if name == "" {
		name = "unnamed"
	}
	return funcPlugin{name: name, onEnable: onEnable, onDisable: onDisable}
}

// AppOption configures an App created by New.
type AppOption func(*App)

// WithDataDir sets the base directory used for per-plugin data folders
// (defaults to "plugins").
func WithDataDir(dir string) AppOption {
	return func(a *App) {
		if dir != "" {
			a.dataDir = filepath.Clean(dir)
		}
	}
}

// WithLogger sets the base logger used to derive per-plugin loggers.
func WithLogger(log *slog.Logger) AppOption {
	return func(a *App) {
		if log != nil {
			a.log = log
		}
	}
}

type pluginEntry struct {
	plugin  Plugin
	ctx     *Context
	enabled bool
}

// App manages plugin lifecycles, server access, and a typed inter-plugin
// service registry.
type App struct {
	srv      *server.Server
	log      *slog.Logger
	dataDir  string
	mu       sync.RWMutex
	plugins  []pluginEntry
	services map[reflect.Type]any
}

// New creates an App wrapping srv with optional configuration.
func New(srv *server.Server, opts ...AppOption) *App {
	a := &App{
		srv:      srv,
		log:      slog.Default(),
		dataDir:  "plugins",
		services: make(map[reflect.Type]any),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(a)
		}
	}
	return a
}

// Use registers and enables each non-nil plugin in order and returns a for chaining.
func (a *App) Use(plugins ...Plugin) *App {
	if a == nil {
		return nil
	}
	for _, p := range plugins {
		if p == nil {
			continue
		}
		ctx := a.newContext(p.Name())
		a.mu.Lock()
		idx := len(a.plugins)
		a.plugins = append(a.plugins, pluginEntry{plugin: p, ctx: ctx})
		a.mu.Unlock()

		p.Enable(ctx)

		a.mu.Lock()
		a.plugins[idx].enabled = true
		a.mu.Unlock()
	}
	return a
}

// Disable disables all enabled plugins in reverse registration order and runs
// every plugin Context's registered cleanup callbacks. Safe to call repeatedly.
func (a *App) Disable() {
	if a == nil {
		return
	}
	a.mu.Lock()
	toDisable := make([]pluginEntry, 0, len(a.plugins))
	for i := len(a.plugins) - 1; i >= 0; i-- {
		if a.plugins[i].enabled {
			a.plugins[i].enabled = false
			toDisable = append(toDisable, a.plugins[i])
		}
	}
	a.mu.Unlock()

	for _, entry := range toDisable {
		entry.plugin.Disable(entry.ctx)
		entry.ctx.cleanup()
	}
}

// Plugins returns a snapshot of registered plugins.
func (a *App) Plugins() []Plugin {
	if a == nil {
		return nil
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]Plugin, len(a.plugins))
	for i, e := range a.plugins {
		out[i] = e.plugin
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

// World returns the server's default Overworld, or nil.
func (a *App) World() *world.World {
	if a == nil || a.srv == nil {
		return nil
	}
	return a.srv.World()
}

// Nether returns the server's Nether world, or nil.
func (a *App) Nether() *world.World {
	if a == nil || a.srv == nil {
		return nil
	}
	return a.srv.Nether()
}

// End returns the server's End world, or nil.
func (a *App) End() *world.World {
	if a == nil || a.srv == nil {
		return nil
	}
	return a.srv.End()
}

// Players yields every online player across worlds, each within its world's
// transaction. Do not call from inside a world owner transaction.
func (a *App) Players() iter.Seq[*player.Player] {
	if a == nil || a.srv == nil {
		return func(func(*player.Player) bool) {}
	}
	return a.srv.Players(nil)
}

// PlayersIn yields online players in tx's world within tx.
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

// Exec runs fn synchronously inside a transaction on w (defaulting to a.World()).
func (a *App) Exec(w *world.World, fn func(tx *world.Tx)) {
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

// Provide registers service value svc under its static type T in app's service
// registry so other plugins can discover it via Service[T].
func Provide[T any](app *App, svc T) {
	if app == nil {
		return
	}
	key := reflect.TypeFor[T]()
	app.mu.Lock()
	app.services[key] = svc
	app.mu.Unlock()
}

// Service looks up a service previously registered with Provide[T] on app.
func Service[T any](app *App) (T, bool) {
	var zero T
	if app == nil {
		return zero, false
	}
	key := reflect.TypeFor[T]()
	app.mu.RLock()
	v, ok := app.services[key]
	app.mu.RUnlock()
	if !ok {
		return zero, false
	}
	typed, ok := v.(T)
	return typed, ok
}

// MustService returns the service registered for type T on app or panics if absent.
func MustService[T any](app *App) T {
	v, ok := Service[T](app)
	if !ok {
		panic(fmt.Sprintf("plugin: service %v not registered", reflect.TypeFor[T]()))
	}
	return v
}

func (a *App) newContext(name string) *Context {
	clean := filepath.Base(strings.TrimSpace(name))
	if clean == "" || clean == "." || clean == ".." || clean == string(filepath.Separator) {
		clean = "default"
	}
	return &Context{
		app:     a,
		name:    name,
		dataDir: filepath.Join(a.dataDir, clean),
		log:     a.log.With("plugin", name),
	}
}

// Context is the scoped handle passed to a single Plugin during Enable and
// Disable. Resources registered with Context (event subscriptions, scheduled
// tasks, opened worlds, packet interceptors) are cleaned up automatically when
// the plugin disables.
type Context struct {
	app      *App
	name     string
	dataDir  string
	log      *slog.Logger
	mu       sync.Mutex
	cleanups []func()
}

// Name returns the plugin's registered name.
func (c *Context) Name() string {
	if c == nil {
		return ""
	}
	return c.name
}

// App returns the owning *App.
func (c *Context) App() *App {
	if c == nil {
		return nil
	}
	return c.app
}

// Server returns the underlying *server.Server.
func (c *Context) Server() *server.Server {
	if c == nil {
		return nil
	}
	return c.app.Server()
}

// Log returns the plugin's structured logger.
func (c *Context) Log() *slog.Logger {
	if c == nil || c.log == nil {
		return slog.Default()
	}
	return c.log
}

// Events returns the server-wide event bus.
func (c *Context) Events() *event.Bus {
	if c == nil {
		return nil
	}
	return c.app.Events()
}

// World returns the server's default Overworld.
func (c *Context) World() *world.World {
	if c == nil {
		return nil
	}
	return c.app.World()
}

// Nether returns the server's Nether world.
func (c *Context) Nether() *world.World {
	if c == nil {
		return nil
	}
	return c.app.Nether()
}

// End returns the server's End world.
func (c *Context) End() *world.World {
	if c == nil {
		return nil
	}
	return c.app.End()
}

// Exec runs fn synchronously inside a transaction on w (defaulting to c.World()).
func (c *Context) Exec(w *world.World, fn func(tx *world.Tx)) {
	if c != nil {
		c.app.Exec(w, fn)
	}
}

// OnDisable registers fn to run automatically when this plugin is disabled
// (executed in LIFO order).
func (c *Context) OnDisable(fn func()) {
	if c == nil || fn == nil {
		return
	}
	c.mu.Lock()
	c.cleanups = append(c.cleanups, fn)
	c.mu.Unlock()
}

func (c *Context) cleanup() {
	if c == nil {
		return
	}
	c.mu.Lock()
	fns := c.cleanups
	c.cleanups = nil
	c.mu.Unlock()
	for i := len(fns) - 1; i >= 0; i-- {
		fns[i]()
	}
}

// DataDir returns the root directory allocated for this plugin's files.
func (c *Context) DataDir() string {
	if c == nil {
		return ""
	}
	return c.dataDir
}

// Path resolves a relative path inside the plugin's data directory, rejecting
// path traversal outside DataDir.
func (c *Context) Path(rel string) (string, error) {
	if c == nil || c.dataDir == "" {
		return "", errors.New("plugin: empty data directory")
	}
	if rel == "" || filepath.IsAbs(rel) {
		return "", fmt.Errorf("plugin: invalid relative path %q", rel)
	}
	full := filepath.Clean(filepath.Join(c.dataDir, rel))
	root := filepath.Clean(c.dataDir)
	if full != root && !strings.HasPrefix(full, root+string(filepath.Separator)) {
		return "", fmt.Errorf("plugin: path %q escapes data directory", rel)
	}
	return full, nil
}

// ReadFile reads raw bytes from rel inside the plugin's data directory.
func (c *Context) ReadFile(rel string) ([]byte, error) {
	p, err := c.Path(rel)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(p)
}

// WriteFile atomically writes data to rel inside the plugin's data directory.
func (c *Context) WriteFile(rel string, data []byte) error {
	p, err := c.Path(rel)
	if err != nil {
		return err
	}
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, p)
}

// DeleteFile deletes rel from the plugin's data directory if present.
func (c *Context) DeleteFile(rel string) error {
	p, err := c.Path(rel)
	if err != nil {
		return err
	}
	err = os.Remove(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// SaveJSON marshals v as indented JSON and writes it atomically to rel.
func SaveJSON[T any](ctx *Context, rel string, v T) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return ctx.WriteFile(rel, append(b, '\n'))
}

// LoadJSON reads and unmarshals JSON from rel into T.
func LoadJSON[T any](ctx *Context, rel string) (T, error) {
	var zero T
	b, err := ctx.ReadFile(rel)
	if err != nil {
		return zero, err
	}
	if err := json.Unmarshal(b, &zero); err != nil {
		return zero, err
	}
	return zero, nil
}

// LoadOrDefaultJSON loads rel if present, or saves and returns def if missing.
func LoadOrDefaultJSON[T any](ctx *Context, rel string, def T) (T, error) {
	v, err := LoadJSON[T](ctx, rel)
	if errors.Is(err, os.ErrNotExist) {
		if saveErr := SaveJSON(ctx, rel, def); saveErr != nil {
			return def, saveErr
		}
		return def, nil
	}
	return v, err
}

// SaveTOML marshals v as TOML and writes it atomically to rel.
func SaveTOML[T any](ctx *Context, rel string, v T) error {
	b, err := toml.Marshal(v)
	if err != nil {
		return err
	}
	return ctx.WriteFile(rel, b)
}

// LoadTOML reads and unmarshals TOML from rel into T.
func LoadTOML[T any](ctx *Context, rel string) (T, error) {
	var zero T
	b, err := ctx.ReadFile(rel)
	if err != nil {
		return zero, err
	}
	if err := toml.Unmarshal(b, &zero); err != nil {
		return zero, err
	}
	return zero, nil
}

// LoadOrDefaultTOML loads rel if present, or saves and returns def if missing.
func LoadOrDefaultTOML[T any](ctx *Context, rel string, def T) (T, error) {
	v, err := LoadTOML[T](ctx, rel)
	if errors.Is(err, os.ErrNotExist) {
		if saveErr := SaveTOML(ctx, rel, def); saveErr != nil {
			return def, saveErr
		}
		return def, nil
	}
	return v, err
}
