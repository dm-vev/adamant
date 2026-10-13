package builtin

import (
	"iter"
	"time"

	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

type serverAdapter interface {
	Players(tx *world.Tx) iter.Seq[*player.Player]
	MaxPlayerCount() int
	Close() error
	World() *world.World
	StartTime() time.Time
	WhitelistEnabled() bool
	WhitelistSetEnabled(enabled bool) error
	WhitelistReload() error
	WhitelistEntries() ([]string, error)
	WhitelistAdd(name string) (bool, error)
	WhitelistRemove(name string) (bool, error)
	IsOperator(name string) bool
	Op(name string) (bool, error)
	Deop(name string) (bool, error)
	OperatorEntries() ([]string, error)
	BanPlayer(name, reason string) (bool, error)
	PardonPlayer(name string) (bool, error)
	BanIP(ip, reason string) (bool, error)
	PardonIP(ip string) (bool, error)
	BanEntries(ip bool) ([]string, error)
	Banned(addr string, name string) (string, bool)
	SaveAll() error
	SaveEnabled(enabled bool)
	SaveEnabledState() bool
	SetIdleTimeout(minutes int)
	IdleTimeout() int
	Version() string
}
