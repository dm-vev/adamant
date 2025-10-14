package plugin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	goplugin "plugin"
	"runtime/debug"
	"slices"
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"log/slog"
)

var pluginFactorySymbols = []string{"InitPlugin", "Init", "NewPlugin", "New"}

type pluginInstance[S any, C any] struct {
	name    string
	version string
	path    string
	plugin  Plugin
	module  *goplugin.Plugin
	api     *API[S, C]
	cancel  context.CancelFunc
}

func (pi pluginInstance[S, C]) info() Info {
	return Info{Name: pi.name, Version: pi.version, Path: pi.path}
}

// Manager coordinates dynamic plugin discovery, loading, and lifecycle management.
type Manager[S any, C any] struct {
	host       Host[S, C]
	cfg        Config
	log        *slog.Logger
	runtimeLog *slog.Logger

	once    sync.Once
	mu      sync.RWMutex
	plugins []pluginInstance[S, C]
	events  *eventHub[S, C]
}

// NewManager constructs a Manager using the provided host and configuration snapshot.
func NewManager[S any, C any](host Host[S, C], cfg Config) *Manager[S, C] {
	manager := &Manager[S, C]{
		host: host,
		cfg: Config{
			Enabled:       cfg.Enabled,
			Directory:     cfg.Directory,
			DataDirectory: cfg.DataDirectory,
			Autoload:      cfg.Autoload,
			Files:         slices.Clone(cfg.Files),
		},
	}
	logger := host.Logger()
	if logger == nil {
		logger = slog.Default()
	}
	manager.log = logger
	manager.runtimeLog = logger.With("subsystem", "plugin.runtime")
	manager.events = newEventHub(manager, logger)
	return manager
}

// Enabled reports whether the plugin subsystem should run.
func (m *Manager[S, C]) Enabled() bool {
	return m.cfg.Enabled
}

// Directory returns the directory searched for plugin binaries.
func (m *Manager[S, C]) Directory() string {
	return m.directory()
}

// DataRoot returns the root directory used for plugin data storage.
func (m *Manager[S, C]) DataRoot() string {
	return m.dataRoot()
}

// ResolvePath resolves path against the configured plugin directory when it is
// not absolute and returns the cleaned result.
func (m *Manager[S, C]) ResolvePath(path string) string {
	return m.resolvePath(path)
}

// LoadConfigured initialises the plugin system and enables plugins based on configuration.
func (m *Manager[S, C]) LoadConfigured() {
	m.once.Do(func() {
		m.loadConfigured()
	})
}

// Infos returns metadata for all loaded plugins.
func (m *Manager[S, C]) Infos() []Info {
	m.mu.RLock()
	defer m.mu.RUnlock()

	infos := make([]Info, len(m.plugins))
	for i, p := range m.plugins {
		infos[i] = p.info()
	}
	return infos
}

// Plugin returns a loaded plugin by its case-insensitive name.
func (m *Manager[S, C]) Plugin(name string) (Plugin, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, p := range m.plugins {
		if strings.EqualFold(p.name, name) {
			return p.plugin, true
		}
	}
	return nil, false
}

// Enable loads and enables a plugin by file path.
func (m *Manager[S, C]) Enable(path string) (info Info, err error) {
	if !m.Enabled() {
		return Info{}, ErrDisabled
	}
	if err := m.ensureDirectory(); err != nil {
		return Info{}, fmt.Errorf("prepare plugin directory: %w", err)
	}
	if err := m.ensureDataRoot(); err != nil {
		return Info{}, fmt.Errorf("prepare plugin data storage: %w", err)
	}

	resolved := m.resolvePath(path)

	m.mu.RLock()
	for _, existing := range m.plugins {
		if existing.path == resolved {
			m.mu.RUnlock()
			return existing.info(), ErrAlreadyLoaded
		}
	}
	m.mu.RUnlock()

	mod, err := goplugin.Open(resolved)
	if err != nil {
		return Info{}, fmt.Errorf("open plugin: %w", err)
	}

	factory, symbol, err := lookupPluginFactory[S, C](mod)
	if err != nil {
		return Info{}, fmt.Errorf("locate plugin factory: %w", err)
	}

	initialName := pluginBaseName(resolved)
	ctx, cancel := context.WithCancel(context.Background())
	api := newAPI(m, m.host, initialName)
	api.setContext(ctx)
	initialDataDir := m.pluginDataDirectory(initialName)
	if err := os.MkdirAll(initialDataDir, 0o755); err != nil {
		return Info{}, fmt.Errorf("create plugin data directory: %w", err)
	}
	api.setDataDirectory(initialDataDir)
	defer func() {
		if err != nil {
			cancel()
			m.events.clear(api.pluginName())
		}
	}()
	inst, err := factory(api)
	if err != nil {
		return Info{}, fmt.Errorf("initialise plugin via %s: %w", symbol, err)
	}
	if inst == nil {
		return Info{}, fmt.Errorf("initialise plugin via %s: factory returned nil", symbol)
	}

	previousName := api.pluginName()
	name := inst.Name()
	if name == "" {
		name = previousName
	}
	api.setName(name)
	if previousName != name {
		m.events.rename(previousName, name)
	}

	if targetDir := m.pluginDataDirectory(name); targetDir != api.DataDirectory() {
		if err := m.migrateDataDirectory(api.DataDirectory(), targetDir); err != nil {
			m.runtimeLog.Error("Migrate plugin data directory.", "plugin", name, "error", err)
		} else {
			api.setDataDirectory(targetDir)
		}
	}

	version := ""
	if v, ok := inst.(VersionedPlugin); ok {
		version = v.Version()
	}

	entry := pluginInstance[S, C]{
		name:    name,
		version: version,
		path:    resolved,
		plugin:  inst,
		module:  mod,
		api:     api,
		cancel:  cancel,
	}

	m.mu.Lock()
	for _, existing := range m.plugins {
		if strings.EqualFold(existing.name, entry.name) {
			m.mu.Unlock()
			if err := entry.plugin.Close(); err != nil {
				m.log.Error("Close conflicting plugin instance.", "error", err, "name", entry.name, "path", resolved)
			}
			return Info{}, fmt.Errorf("%w: %s", ErrNameConflict, entry.name)
		}
	}
	m.plugins = append(m.plugins, entry)
	m.mu.Unlock()

	attrs := []any{"name", entry.name, "path", entry.path}
	if entry.version != "" {
		attrs = append(attrs, "version", entry.version)
	}
	attrs = append(attrs, "symbol", symbol)
	m.log.Info("Plugin enabled.", attrs...)

	return entry.info(), nil
}

// Disable disables a plugin by its case-insensitive name and removes it from the manager.
func (m *Manager[S, C]) Disable(name string) (Info, error) {
	if !m.Enabled() {
		return Info{}, ErrDisabled
	}

	m.mu.Lock()
	index := -1
	var entry pluginInstance[S, C]
	for i, p := range m.plugins {
		if strings.EqualFold(p.name, name) {
			index = i
			entry = p
			m.plugins = append(m.plugins[:i], m.plugins[i+1:]...)
			break
		}
	}
	m.mu.Unlock()

	if index == -1 {
		return Info{}, ErrNotFound
	}

	if err := entry.plugin.Close(); err != nil {
		m.mu.Lock()
		m.plugins = append(m.plugins, entry)
		m.mu.Unlock()
		return Info{}, fmt.Errorf("close plugin: %w", err)
	}

	if entry.cancel != nil {
		entry.cancel()
	}
	m.events.clear(entry.name)

	m.log.Info("Plugin disabled.", "name", entry.name, "path", entry.path)
	return entry.info(), nil
}

// Reload disables and then re-enables a plugin by name.
func (m *Manager[S, C]) Reload(name string) (Info, error) {
	info, err := m.Disable(name)
	if err != nil {
		return Info{}, err
	}

	reloaded, err := m.Enable(info.Path)
	if err != nil {
		return Info{}, err
	}

	attrs := []any{"name", reloaded.Name, "path", reloaded.Path}
	if reloaded.Version != "" {
		attrs = append(attrs, "version", reloaded.Version)
	}
	m.log.Info("Plugin reloaded.", attrs...)
	return reloaded, nil
}

// DisableAll disables all currently loaded plugins in reverse load order.
// The returned slice contains metadata for every plugin that was disabled in
// the order the operations were performed.
func (m *Manager[S, C]) DisableAll() ([]Info, error) {
	if !m.Enabled() {
		return nil, ErrDisabled
	}

	m.mu.RLock()
	names := make([]string, len(m.plugins))
	for i, p := range m.plugins {
		names[i] = p.name
	}
	m.mu.RUnlock()

	infos := make([]Info, 0, len(names))
	for i := len(names) - 1; i >= 0; i-- {
		info, err := m.Disable(names[i])
		if err != nil {
			return infos, err
		}
		infos = append(infos, info)
	}
	return infos, nil
}

// Shutdown disables all plugins in reverse load order.
func (m *Manager[S, C]) Shutdown() {
	m.mu.Lock()
	plugins := slices.Clone(m.plugins)
	m.plugins = nil
	m.mu.Unlock()

	for i := len(plugins) - 1; i >= 0; i-- {
		entry := plugins[i]
		if entry.cancel != nil {
			entry.cancel()
		}
		m.events.clear(entry.name)
		if err := entry.plugin.Close(); err != nil {
			m.log.Error("disable plugin", "error", err, "name", entry.name, "path", entry.path)
			continue
		}
		m.log.Info("Plugin disabled.", "name", entry.name, "path", entry.path)
	}
}

func (m *Manager[S, C]) loadConfigured() {
	cfg := m.cfg
	if !cfg.Enabled {
		m.log.Debug("Plugin system disabled.")
		return
	}

	dir := m.directory()
	if err := m.ensureDirectory(); err != nil {
		m.log.Error("create plugin directory", "error", err, "dir", dir)
		return
	}

	seen := map[string]struct{}{}
	var paths []string

	if cfg.Autoload {
		entries, err := os.ReadDir(dir)
		if err != nil {
			m.log.Error("read plugin directory", "error", err, "dir", dir)
		} else {
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				if !strings.EqualFold(filepath.Ext(entry.Name()), ".so") {
					continue
				}
				path := filepath.Join(dir, entry.Name())
				path = filepath.Clean(path)
				if _, ok := seen[path]; ok {
					continue
				}
				seen[path] = struct{}{}
				paths = append(paths, path)
			}
		}
	}

	for _, file := range cfg.Files {
		path := m.resolvePath(file)
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}

	if len(paths) == 0 {
		m.log.Debug("No plugins discovered.")
		return
	}

	slices.Sort(paths)
	for _, path := range paths {
		if _, err := m.Enable(path); err != nil {
			m.log.Error("Enable plugin.", "error", err, "path", path)
		}
	}
}

func (m *Manager[S, C]) directory() string {
	if m.cfg.Directory == "" {
		return "plugins"
	}
	return m.cfg.Directory
}

func (m *Manager[S, C]) ensureDirectory() error {
	return os.MkdirAll(m.directory(), 0o755)
}

func (m *Manager[S, C]) resolvePath(path string) string {
	if path == "" {
		return ""
	}

	cleaned := filepath.Clean(path)
	if filepath.IsAbs(cleaned) {
		return cleaned
	}

	dir := filepath.Clean(m.directory())
	if cleaned == dir {
		return dir
	}

	// Avoid double-joining the plugin directory when the caller already provided a
	// path relative to it (for example "plugins/demo.so").
	if rel, err := filepath.Rel(dir, cleaned); err == nil && rel != ".." && !strings.HasPrefix(rel, fmt.Sprintf("..%c", filepath.Separator)) {
		return cleaned
	}

	return filepath.Clean(filepath.Join(dir, cleaned))
}

func (m *Manager[S, C]) dataRoot() string {
	dir := m.cfg.DataDirectory
	if dir == "" {
		dir = filepath.Join(m.directory(), "data")
	} else if !filepath.IsAbs(dir) {
		dir = filepath.Join(m.directory(), dir)
	}
	return filepath.Clean(dir)
}

func (m *Manager[S, C]) ensureDataRoot() error {
	return os.MkdirAll(m.dataRoot(), 0o755)
}

func (m *Manager[S, C]) pluginDataDirectory(name string) string {
	safe := sanitizePluginDirectory(name)
	return filepath.Join(m.dataRoot(), safe)
}

func (m *Manager[S, C]) migrateDataDirectory(from, to string) error {
	if from == to {
		return nil
	}
	if to == "" {
		return fmt.Errorf("empty target data directory")
	}
	if from == "" {
		return os.MkdirAll(to, 0o755)
	}
	info, err := os.Stat(from)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return os.MkdirAll(to, 0o755)
		}
		return fmt.Errorf("stat source data directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("source data directory is not a directory")
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return fmt.Errorf("ensure target parent: %w", err)
	}
	if err := os.Rename(from, to); err != nil {
		return fmt.Errorf("rename data directory: %w", err)
	}
	return nil
}

func (m *Manager[S, C]) handlePluginPanic(name string, reason any) {
	pluginName := name
	if pluginName == "" {
		pluginName = "plugin"
	}
	stack := debug.Stack()
	m.events.clear(pluginName)
	m.runtimeLog.Error("Plugin panic.", "plugin", pluginName, "panic", reason, "stack", string(stack))
	go func() {
		info, err := m.Disable(pluginName)
		if err != nil {
			if !errors.Is(err, ErrNotFound) {
				m.runtimeLog.Error("Disable panic plugin.", "plugin", pluginName, "error", err)
			}
			return
		}
		attrs := []any{"name", info.Name, "path", info.Path}
		if info.Version != "" {
			attrs = append(attrs, "version", info.Version)
		}
		m.runtimeLog.Warn("Plugin disabled after panic.", attrs...)
	}()
}

func pluginBaseName(path string) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	if ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	base = strings.TrimSpace(base)
	if base == "" {
		return "plugin"
	}
	return base
}

func sanitizePluginDirectory(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "plugin"
	}
	lower := strings.ToLower(trimmed)
	sanitized := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '-' || r == '_' || r == '.':
			return r
		default:
			return '-'
		}
	}, lower)
	sanitized = strings.Trim(sanitized, "-_.")
	if sanitized == "" {
		return "plugin"
	}
	return sanitized
}

func lookupPluginFactory[S any, C any](mod *goplugin.Plugin) (PluginFactory[S, C], string, error) {
	for _, symbol := range pluginFactorySymbols {
		factory, err := exportPluginFactory[S, C](mod, symbol)
		if err != nil {
			if errors.Is(err, errSymbolNotFound) {
				continue
			}
			return nil, symbol, err
		}
		return factory, symbol, nil
	}
	return nil, "", fmt.Errorf("no compatible factory symbol found")
}

var errSymbolNotFound = errors.New("symbol not found")

func exportPluginFactory[S any, C any](mod *goplugin.Plugin, symbol string) (PluginFactory[S, C], error) {
	sym, err := mod.Lookup(symbol)
	if err != nil {
		return nil, errSymbolNotFound
	}
	switch fn := sym.(type) {
	case PluginFactory[S, C]:
		return fn, nil
	case *PluginFactory[S, C]:
		return *fn, nil
	case func(*API[S, C]) (Plugin, error):
		return PluginFactory[S, C](fn), nil
	case *func(*API[S, C]) (Plugin, error):
		return PluginFactory[S, C](*fn), nil
	case func(*API[S, C]) Plugin:
		return func(api *API[S, C]) (Plugin, error) {
			p := fn(api)
			if p == nil {
				return nil, fmt.Errorf("%s returned nil plugin", symbol)
			}
			return p, nil
		}, nil
	case *func(*API[S, C]) Plugin:
		ctor := *fn
		return func(api *API[S, C]) (Plugin, error) {
			p := ctor(api)
			if p == nil {
				return nil, fmt.Errorf("%s returned nil plugin", symbol)
			}
			return p, nil
		}, nil
	default:
		return nil, fmt.Errorf("symbol %s has incompatible type %T", symbol, sym)
	}
}

// PlayerHandlerWrap wraps the provided handler so plugin callbacks are invoked alongside existing logic.
func (m *Manager[S, C]) PlayerHandlerWrap(p *player.Player, base player.Handler) player.Handler {
	return m.events.wrapPlayer(p, base)
}

// WorldHandlerWrap wraps the world handler to invoke plugin callbacks.
func (m *Manager[S, C]) WorldHandlerWrap(w *world.World, base world.Handler) world.Handler {
	return m.events.wrapWorld(w, base)
}

// InventoryHandlerWrap wraps the inventory handler to invoke plugin callbacks.
func (m *Manager[S, C]) InventoryHandlerWrap(inv *inventory.Inventory, base inventory.Handler) inventory.Handler {
	return m.events.wrapInventory(inv, base)
}
