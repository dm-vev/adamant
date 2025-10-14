package builtin

import (
	"iter"
	"time"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

type serverAdapter interface {
	Players(tx *world.Tx) iter.Seq[*player.Player]
	MaxPlayerCount() int
	Close() error
	World() *world.World
	StartTime() time.Time
	Plugins() []server.PluginInfo
	EnablePlugin(path string) (server.PluginInfo, error)
	DisablePlugin(name string) (server.PluginInfo, error)
	ReloadPlugin(name string) (server.PluginInfo, error)
	PluginsEnabled() bool
}
