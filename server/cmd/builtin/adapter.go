package builtin

import (
	"time"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/world"
)

type serverAdapter interface {
	// Online returns the players online, in the order they joined, without
	// waiting for any world.
	Online() []server.OnlinePlayer
	MaxPlayerCount() int
	Close() error
	World() *world.World
	Nether() *world.World
	End() *world.World
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
	// SaveAllFrom saves every dimension without waiting for it in tx, the
	// transaction the command runs in; done runs in tx's world once they
	// were saved.
	SaveAllFrom(tx *world.Tx, done func(tx *world.Tx, err error))
	SaveEnabled(enabled bool)
	SaveEnabledState() bool
	SetIdleTimeout(minutes int)
	IdleTimeout() int
	Version() string
}
