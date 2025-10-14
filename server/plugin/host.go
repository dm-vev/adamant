package plugin

import (
	"time"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
	"iter"
	"log/slog"
)

// Host exposes the subset of server functionality required by the plugin
// manager and APIs.
type Host[S any, C any] interface {
	// Instance returns the underlying server value.
	Instance() S
	// Config returns a snapshot of the server configuration.
	Config() C
	// Logger returns the logger used for structured diagnostics.
	Logger() *slog.Logger
	// StartTime reports the time the server started listening for connections.
	StartTime() time.Time
	// Listen starts running the server's listeners.
	Listen()
	// Accept exposes the server's accept iterator so plugins can observe joins.
	Accept() iter.Seq[*player.Player]
	// World returns the overworld managed by the server.
	World() *world.World
	// Nether returns the nether world managed by the server.
	Nether() *world.World
	// End returns the end world managed by the server.
	End() *world.World
	// MaxPlayerCount returns the configured player cap.
	MaxPlayerCount() int
	// PlayerCount returns the number of currently connected players.
	PlayerCount() int
	// Players exposes the server's player iterator.
	Players(tx *world.Tx) iter.Seq[*player.Player]
	// Player looks up an online player by their UUID.
	Player(uuid uuid.UUID) (*world.EntityHandle, bool)
	// PlayerByName looks up an online player by name.
	PlayerByName(name string) (*world.EntityHandle, bool)
	// PlayerByXUID looks up an online player by XUID.
	PlayerByXUID(xuid string) (*world.EntityHandle, bool)
	// ExecuteCommand runs a command on behalf of the given source.
	ExecuteCommand(source cmd.Source, commandLine string)
	// PlayerSummaries returns metadata about all currently connected players.
	PlayerSummaries() []PlayerSummary
	// CloseOnProgramEnd closes the server when the program receives termination signals.
	CloseOnProgramEnd()
	// Close shuts the underlying server down.
	Close() error
	// LoadPlugins triggers discovery and activation for configured plugins.
	LoadPlugins()
	// PluginsEnabled reports if the plugin system is active.
	PluginsEnabled() bool
}
