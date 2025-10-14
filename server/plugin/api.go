package plugin

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/chat"
	"github.com/df-mc/dragonfly/server/player/form"
	"github.com/df-mc/dragonfly/server/player/title"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
	"iter"
	"log/slog"
)

// API exposes functionality of the server core to dynamically loaded plugins.
type API[S any, C any] struct {
	manager *Manager[S, C]
	host    Host[S, C]
	name    atomic.Value // stores string
	ctx     atomic.Value // stores context.Context
	dataDir atomic.Value // stores string
}

func newAPI[S any, C any](manager *Manager[S, C], host Host[S, C], name string) *API[S, C] {
	api := &API[S, C]{manager: manager, host: host}
	api.name.Store(name)
	api.ctx.Store(context.Background())
	return api
}

func (api *API[S, C]) setName(name string) {
	if name == "" {
		return
	}
	api.name.Store(name)
}

func (api *API[S, C]) pluginName() string {
	if v := api.name.Load(); v != nil {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return "plugin"
}

func (api *API[S, C]) setContext(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	api.ctx.Store(ctx)
}

// Context returns a cancellable context that is invalidated when the plugin is disabled.
func (api *API[S, C]) Context() context.Context {
	if v := api.ctx.Load(); v != nil {
		if ctx, ok := v.(context.Context); ok && ctx != nil {
			return ctx
		}
	}
	return context.Background()
}

func (api *API[S, C]) setDataDirectory(dir string) {
	if dir == "" {
		api.dataDir.Store("")
		return
	}
	api.dataDir.Store(filepath.Clean(dir))
}

// DataDirectory returns the absolute path to the plugin's data directory.
func (api *API[S, C]) DataDirectory() string {
	if v := api.dataDir.Load(); v != nil {
		if dir, ok := v.(string); ok && dir != "" {
			return dir
		}
	}
	return api.manager.pluginDataDirectory(api.pluginName())
}

func (api *API[S, C]) resolveDataPath(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("data path is empty")
	}
	if filepath.IsAbs(name) {
		return "", fmt.Errorf("data path must be relative")
	}
	base := api.DataDirectory()
	cleaned := filepath.Clean(name)
	target := filepath.Join(base, cleaned)
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("data path escapes plugin directory")
	}
	return target, nil
}

// EnsureDataSubdir ensures a subdirectory inside the plugin data directory exists and returns its absolute path.
func (api *API[S, C]) EnsureDataSubdir(name string) (string, error) {
	if name == "" {
		dir := api.DataDirectory()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
		return dir, nil
	}
	path, err := api.resolveDataPath(name)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return "", err
	}
	return path, nil
}

// OpenDataFile opens or creates a file within the plugin data directory using the provided flags and permissions.
func (api *API[S, C]) OpenDataFile(name string, flag int, perm fs.FileMode) (*os.File, error) {
	path, err := api.resolveDataPath(name)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if perm == 0 {
		perm = 0o644
	}
	return os.OpenFile(path, flag, perm)
}

// Go launches fn on a new goroutine tied to the plugin's lifecycle context. Panics cause the plugin to be disabled.
func (api *API[S, C]) Go(fn func(context.Context)) {
	if fn == nil {
		return
	}
	ctx := api.Context()
	name := api.pluginName()
	go func() {
		defer func() {
			if r := recover(); r != nil {
				api.manager.handlePluginPanic(name, r)
			}
		}()
		fn(ctx)
	}()
}

// Server returns the underlying server instance.
func (api *API[S, C]) Server() S {
	return api.host.Instance()
}

// Config returns a snapshot of the server configuration at the time of the call.
func (api *API[S, C]) Config() C {
	return api.host.Config()
}

// StartTime reports when the server started listening for connections.
func (api *API[S, C]) StartTime() time.Time {
	return api.host.StartTime()
}

// Listen starts the server listeners to accept new connections.
func (api *API[S, C]) Listen() {
	api.host.Listen()
}

// Accept yields players as they join the server until shutdown.
func (api *API[S, C]) Accept() iter.Seq[*player.Player] {
	return api.host.Accept()
}

// Logger returns a logger scoped to the plugin's name for structured logging.
func (api *API[S, C]) Logger() *slog.Logger {
	logger := api.host.Logger()
	if logger == nil {
		logger = slog.Default()
	}
	return logger.With("plugin", api.pluginName())
}

// World returns the overworld managed by the server.
func (api *API[S, C]) World() *world.World {
	return api.host.World()
}

// Nether returns the nether world managed by the server.
func (api *API[S, C]) Nether() *world.World {
	return api.host.Nether()
}

// End returns the end world managed by the server.
func (api *API[S, C]) End() *world.World {
	return api.host.End()
}

// MaxPlayerCount returns the configured player cap, accounting for dynamic limits.
func (api *API[S, C]) MaxPlayerCount() int {
	return api.host.MaxPlayerCount()
}

// PlayerCount returns the number of players currently connected to the server.
func (api *API[S, C]) PlayerCount() int {
	return api.host.PlayerCount()
}

// CloseOnProgramEnd registers a shutdown handler to close the server on termination signals.
func (api *API[S, C]) CloseOnProgramEnd() {
	api.host.CloseOnProgramEnd()
}

// Players exposes the server's player iterator. See the server package for usage semantics.
func (api *API[S, C]) Players(tx *world.Tx) iter.Seq[*player.Player] {
	return api.host.Players(tx)
}

// Player looks up a player by UUID and returns an entity handle if found.
func (api *API[S, C]) Player(id uuid.UUID) (*world.EntityHandle, bool) {
	return api.host.Player(id)
}

// PlayerByName looks up an online player by their name.
func (api *API[S, C]) PlayerByName(name string) (*world.EntityHandle, bool) {
	return api.host.PlayerByName(name)
}

// PlayerByXUID looks up an online player by their XUID.
func (api *API[S, C]) PlayerByXUID(xuid string) (*world.EntityHandle, bool) {
	return api.host.PlayerByXUID(xuid)
}

// PlayerSummaries returns metadata snapshots for all connected players.
func (api *API[S, C]) PlayerSummaries() []PlayerSummary {
	return api.host.PlayerSummaries()
}

// PlayerSummary returns metadata for a specific player by UUID.
func (api *API[S, C]) PlayerSummary(id uuid.UUID) (PlayerSummary, bool) {
	for _, summary := range api.PlayerSummaries() {
		if summary.UUID == id {
			return summary, true
		}
	}
	return PlayerSummary{}, false
}

// PlayerLatency returns the network latency reported for the player with the provided UUID.
func (api *API[S, C]) PlayerLatency(id uuid.UUID) (time.Duration, bool) {
	var latency time.Duration
	ok := api.withPlayerByUUID(id, func(p *player.Player) {
		latency = p.Latency()
	})
	return latency, ok
}

// RegisterCommand registers a command with the global command registry.
func (api *API[S, C]) RegisterCommand(command cmd.Command) {
	cmd.Register(command)
}

// Commands returns all registered commands indexed by alias.
func (api *API[S, C]) Commands() map[string]cmd.Command {
	return cmd.Commands()
}

// ExecuteCommand executes a command line on behalf of the provided source. The command line should include the leading slash.
func (api *API[S, C]) ExecuteCommand(source cmd.Source, commandLine string) {
	api.host.ExecuteCommand(source, commandLine)
}

// Broadcast writes a raw string message to the global chat.
func (api *API[S, C]) Broadcast(message string) {
	_, _ = chat.Global.WriteString(message)
}

// BroadcastTranslation broadcasts a translated chat message using the supplied translation template.
func (api *API[S, C]) BroadcastTranslation(t chat.Translation, args ...any) {
	chat.Global.Writet(t, args...)
}

// SubscribeChat subscribes a chat subscriber to the global chat stream.
func (api *API[S, C]) SubscribeChat(sub chat.Subscriber) {
	chat.Global.Subscribe(sub)
}

// UnsubscribeChat removes a subscriber from the global chat stream.
func (api *API[S, C]) UnsubscribeChat(sub chat.Subscriber) {
	chat.Global.Unsubscribe(sub)
}

// Plugins returns metadata for all currently loaded plugins.
func (api *API[S, C]) Plugins() []Info {
	return api.manager.Infos()
}

// Plugin returns a loaded plugin by name if present.
func (api *API[S, C]) Plugin(name string) (Plugin, bool) {
	return api.manager.Plugin(name)
}

// EnablePlugin loads and enables a plugin by file path.
func (api *API[S, C]) EnablePlugin(path string) (Info, error) {
	return api.manager.Enable(path)
}

// DisablePlugin disables a plugin by its name.
func (api *API[S, C]) DisablePlugin(name string) (Info, error) {
	return api.manager.Disable(name)
}

// ReloadPlugin reloads a plugin by disabling and re-enabling it.
func (api *API[S, C]) ReloadPlugin(name string) (Info, error) {
	return api.manager.Reload(name)
}

// MessagePlayer sends a formatted chat message to the player with the provided UUID.
func (api *API[S, C]) MessagePlayer(id uuid.UUID, message ...any) bool {
	return api.withPlayerByUUID(id, func(p *player.Player) {
		p.Message(message...)
	})
}

// MessagePlayerTranslation sends a translated chat message to the target player.
func (api *API[S, C]) MessagePlayerTranslation(id uuid.UUID, t chat.Translation, args ...any) bool {
	return api.withPlayerByUUID(id, func(p *player.Player) {
		p.Messaget(t, args...)
	})
}

// SendPopup sends a popup message above the hotbar of the specified player.
func (api *API[S, C]) SendPopup(id uuid.UUID, message ...any) bool {
	return api.withPlayerByUUID(id, func(p *player.Player) {
		p.SendPopup(message...)
	})
}

// SendTip sends a tip message in the middle of the screen of the specified player.
func (api *API[S, C]) SendTip(id uuid.UUID, message ...any) bool {
	return api.withPlayerByUUID(id, func(p *player.Player) {
		p.SendTip(message...)
	})
}

// SendTitle sends a title to the specified player.
func (api *API[S, C]) SendTitle(id uuid.UUID, t title.Title) bool {
	return api.withPlayerByUUID(id, func(p *player.Player) {
		p.SendTitle(t)
	})
}

// SendForm sends a form to the specified player. The form will be queued if the client is already processing another form.
func (api *API[S, C]) SendForm(id uuid.UUID, f form.Form) bool {
	if f == nil {
		return false
	}
	return api.withPlayerByUUID(id, func(p *player.Player) {
		p.SendForm(f)
	})
}

// DisconnectPlayer disconnects the player with the provided UUID using the optional reason.
func (api *API[S, C]) DisconnectPlayer(id uuid.UUID, reason ...any) bool {
	return api.withPlayerByUUID(id, func(p *player.Player) {
		if len(reason) == 0 {
			_ = p.Close()
			return
		}
		p.Disconnect(reason...)
	})
}

// CloseServer requests a graceful server shutdown.
func (api *API[S, C]) CloseServer() error {
	return api.host.Close()
}

// LoadPlugins triggers plugin discovery and loading based on configuration.
func (api *API[S, C]) LoadPlugins() {
	api.host.LoadPlugins()
}

// PluginsEnabled reports whether the plugin subsystem is currently active.
func (api *API[S, C]) PluginsEnabled() bool {
	return api.host.PluginsEnabled()
}

// PluginDirectory returns the directory scanned for plugin binaries.
func (api *API[S, C]) PluginDirectory() string {
	return api.manager.Directory()
}

// PluginDataRoot returns the root directory used to persist plugin data.
func (api *API[S, C]) PluginDataRoot() string {
	return api.manager.DataRoot()
}

// ResolvePluginPath resolves the provided path against the configured plugin directory.
func (api *API[S, C]) ResolvePluginPath(path string) string {
	return api.manager.ResolvePath(path)
}

// DisableAllPlugins disables every currently loaded plugin and returns metadata for each.
func (api *API[S, C]) DisableAllPlugins() ([]Info, error) {
	return api.manager.DisableAll()
}

// Events returns helpers for subscribing to player, world, and inventory events.
func (api *API[S, C]) Events() *PluginEvents[S, C] {
	return &PluginEvents[S, C]{api: api}
}

// PluginEvents exposes registration helpers for subscribing to core event streams.
type PluginEvents[S any, C any] struct {
	api *API[S, C]
}

// OnPlayer registers a player.Handler that is invoked for every player event.
// The returned function removes the handler when called.
func (pe *PluginEvents[S, C]) OnPlayer(handler player.Handler) func() {
	if pe == nil || handler == nil {
		return func() {}
	}
	return pe.api.manager.events.addPlayer(pe.api.pluginName(), handler)
}

// OnWorld registers a world.Handler invoked for each world managed by the server.
// The returned function removes the handler when called.
func (pe *PluginEvents[S, C]) OnWorld(handler world.Handler) func() {
	if pe == nil || handler == nil {
		return func() {}
	}
	return pe.api.manager.events.addWorld(pe.api.pluginName(), handler)
}

// OnInventory registers an inventory.Handler that observes inventory events.
// The returned function removes the handler when called.
func (pe *PluginEvents[S, C]) OnInventory(handler inventory.Handler) func() {
	if pe == nil || handler == nil {
		return func() {}
	}
	return pe.api.manager.events.addInventory(pe.api.pluginName(), handler)
}

// Clear removes all handlers previously registered by the plugin.
func (pe *PluginEvents[S, C]) Clear() {
	if pe == nil {
		return
	}
	pe.api.manager.events.clear(pe.api.pluginName())
}

func (api *API[S, C]) withPlayerByUUID(id uuid.UUID, fn func(*player.Player)) bool {
	if fn == nil {
		return false
	}
	handle, ok := api.host.Player(id)
	if !ok {
		return false
	}
	return api.withPlayerHandle(handle, fn)
}

func (api *API[S, C]) withPlayerHandle(handle *world.EntityHandle, fn func(*player.Player)) bool {
	if handle == nil || fn == nil {
		return false
	}
	executed := false
	ok := handle.ExecWorld(func(tx *world.Tx, entity world.Entity) {
		if p, ok := entity.(*player.Player); ok {
			fn(p)
			executed = true
		}
	})
	return ok && executed
}
