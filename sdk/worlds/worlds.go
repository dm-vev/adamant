// Package worlds manages dynamic world lifecycle (creation, registry lookup,
// unloading, automatic plugin-scoped shutdown) and checkpoint-safe cross-world
// player transfers.
package worlds

import (
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"

	"github.com/df-mc/dragonfly/sdk/plugin"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/mcdb"
	"github.com/go-gl/mathgl/mgl64"
)

type worldConfig struct {
	dim             world.Dimension
	gen             world.Generator
	provider        world.Provider
	directory       string
	blocks          world.BlockRegistry
	entities        world.EntityRegistry
	readOnly        bool
	synchronous     bool
	randomTickSpeed int
	log             *slog.Logger
}

// Option configures a world opened by Open or Manager.Open.
type Option func(*worldConfig)

// Dimension sets the world dimension (defaults to world.Overworld).
func Dimension(d world.Dimension) Option { return func(c *worldConfig) { c.dim = d } }

// Generator sets the chunk generator (defaults to world.NopGenerator{}).
func Generator(g world.Generator) Option { return func(c *worldConfig) { c.gen = g } }

// Provider sets a custom persistence provider (overrides Directory).
func Provider(p world.Provider) Option { return func(c *worldConfig) { c.provider = p } }

// Directory opens an mcdb persistence provider at dir when Provider is unset.
func Directory(dir string) Option { return func(c *worldConfig) { c.directory = dir } }

// Blocks overrides the block registry (defaults to world.DefaultBlockRegistry).
func Blocks(b world.BlockRegistry) Option { return func(c *worldConfig) { c.blocks = b } }

// Entities overrides the entity registry (defaults to entity.DefaultRegistry).
func Entities(e world.EntityRegistry) Option { return func(c *worldConfig) { c.entities = e } }

// ReadOnly prevents writing chunk modifications back to the provider.
func ReadOnly() Option { return func(c *worldConfig) { c.readOnly = true } }

// Synchronous runs the world tick loop synchronously (useful for tests/tooling).
func Synchronous() Option { return func(c *worldConfig) { c.synchronous = true } }

// RandomTickSpeed configures random tick speed (0 uses vanilla default, -1 disables).
func RandomTickSpeed(v int) Option { return func(c *worldConfig) { c.randomTickSpeed = v } }

// Logger overrides the logger used by the world.
func Logger(l *slog.Logger) Option { return func(c *worldConfig) { c.log = l } }

func openWorld(name string, opts ...Option) (*world.World, error) {
	var cfg worldConfig
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	prov := cfg.provider
	if prov == nil {
		if cfg.directory != "" {
			db, err := mcdb.Open(cfg.directory)
			if err != nil {
				return nil, fmt.Errorf("sdk/worlds: open mcdb %q: %w", name, err)
			}
			prov = db
		} else {
			prov = world.NopProvider{}
		}
	}
	dim := cfg.dim
	if dim == nil {
		dim = world.Overworld
	}
	gen := cfg.gen
	if gen == nil {
		gen = world.NopGenerator{}
	}
	entities := cfg.entities
	if len(entities.Types()) == 0 {
		entities = entity.DefaultRegistry
	}
	blocks := cfg.blocks
	if blocks == nil {
		blocks = world.DefaultBlockRegistry
	}
	return world.Config{
		Log:             cfg.log,
		Dim:             dim,
		Provider:        prov,
		Generator:       gen,
		Blocks:          blocks,
		Entities:        entities,
		ReadOnly:        cfg.readOnly,
		Synchronous:     cfg.synchronous,
		RandomTickSpeed: cfg.randomTickSpeed,
	}.New(), nil
}

// Open opens a new world with opts. If ctx is non-nil, the world is
// automatically closed when the owning plugin is disabled.
func Open(ctx *plugin.Context, name string, opts ...Option) (*world.World, error) {
	if name == "" {
		return nil, errors.New("sdk/worlds: world name must not be empty")
	}
	w, err := openWorld(name, opts...)
	if err != nil {
		return nil, err
	}
	if ctx != nil {
		ctx.OnDisable(func() { _ = w.Close() })
	}
	return w, nil
}

type managedWorld struct {
	w     *world.World
	owned bool
}

// Manager tracks named dynamic worlds alongside an App's built-in dimensions
// ("overworld", "nether", "end").
type Manager struct {
	app    *plugin.App
	mu     sync.RWMutex
	worlds map[string]managedWorld
}

// NewManager creates a Manager backed by app (may be nil if only custom worlds
// are needed).
func NewManager(app *plugin.App) *Manager {
	return &Manager{
		app:    app,
		worlds: make(map[string]managedWorld),
	}
}

// NewScopedManager creates a Manager whose owned worlds are automatically
// closed when ctx's plugin is disabled.
func NewScopedManager(ctx *plugin.Context) *Manager {
	var app *plugin.App
	if ctx != nil {
		app = ctx.App()
	}
	m := NewManager(app)
	if ctx != nil {
		ctx.OnDisable(m.CloseAll)
	}
	return m
}

// Open creates, registers, and takes Close ownership of a new world under name.
func (m *Manager) Open(name string, opts ...Option) (*world.World, error) {
	if m == nil {
		return nil, errors.New("sdk/worlds: nil Manager")
	}
	if name == "" {
		return nil, errors.New("sdk/worlds: world name must not be empty")
	}
	m.mu.RLock()
	_, exists := m.worlds[name]
	m.mu.RUnlock()
	if exists {
		return nil, fmt.Errorf("sdk/worlds: world %q already exists", name)
	}

	w, err := openWorld(name, opts...)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	if _, exists := m.worlds[name]; exists {
		m.mu.Unlock()
		_ = w.Close()
		return nil, fmt.Errorf("sdk/worlds: world %q already exists", name)
	}
	m.worlds[name] = managedWorld{w: w, owned: true}
	m.mu.Unlock()
	return w, nil
}

// Register registers an externally constructed world under name without taking
// Close ownership.
func (m *Manager) Register(name string, w *world.World) error {
	if m == nil || name == "" || w == nil {
		return errors.New("sdk/worlds: Register requires non-nil Manager, non-empty name, and non-nil World")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.worlds[name]; exists {
		return fmt.Errorf("sdk/worlds: world %q already registered", name)
	}
	m.worlds[name] = managedWorld{w: w, owned: false}
	return nil
}

// ByName returns a dynamic world by name, or one of the server's built-in
// dimensions ("overworld", "nether", "end") when backed by an App.
func (m *Manager) ByName(name string) (*world.World, bool) {
	if m == nil || name == "" {
		return nil, false
	}
	m.mu.RLock()
	entry, ok := m.worlds[name]
	m.mu.RUnlock()
	if ok && entry.w != nil {
		return entry.w, true
	}
	if m.app == nil {
		return nil, false
	}
	switch name {
	case "overworld":
		w := m.app.World()
		return w, w != nil
	case "nether":
		w := m.app.Nether()
		return w, w != nil
	case "end":
		w := m.app.End()
		return w, w != nil
	default:
		return nil, false
	}
}

// Names returns a sorted slice of names for all dynamically managed worlds.
func (m *Manager) Names() []string {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.worlds))
	for name := range m.worlds {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Unload unregisters name and closes the world if it was created by Open.
func (m *Manager) Unload(name string) error {
	if m == nil || name == "" {
		return errors.New("sdk/worlds: Unload requires non-nil Manager and non-empty name")
	}
	m.mu.Lock()
	entry, ok := m.worlds[name]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("sdk/worlds: world %q not found", name)
	}
	delete(m.worlds, name)
	m.mu.Unlock()
	if entry.owned && entry.w != nil {
		return entry.w.Close()
	}
	return nil
}

// CloseAll unregisters all worlds and closes every world created by Open.
func (m *Manager) CloseAll() {
	if m == nil {
		return
	}
	m.mu.Lock()
	owned := make([]*world.World, 0, len(m.worlds))
	for name, entry := range m.worlds {
		if entry.owned && entry.w != nil {
			owned = append(owned, entry.w)
		}
		delete(m.worlds, name)
	}
	m.mu.Unlock()
	for _, w := range owned {
		_ = w.Close()
	}
}

// Transfer safely moves p from tx's world to target and teleports them to pos
// using Adamant's checkpoint-safe world.Transfer protocol. If target is already
// the player's current world, it simply teleports p to pos in tx. Returns false
// if a checkpoint temporarily holds transfers out of tx's world.
func Transfer(p *player.Player, target *world.World, pos mgl64.Vec3, tx *world.Tx) bool {
	if p == nil || target == nil || tx == nil || tx.Closed() {
		return false
	}
	if tx.World() == target {
		p.Teleport(pos)
		return true
	}
	tr, ok := world.BeginTransfer(tx)
	if !ok {
		return false
	}
	h := tr.Remove(tx, p)
	if h == nil {
		tr.Cancel()
		return false
	}
	target.Exec(func(dst *world.Tx) {
		ent := dst.AddEntity(h)
		if moved, ok := ent.(*player.Player); ok {
			moved.Teleport(pos)
		}
	})
	return true
}
