package builtin

import (
	"context"
	"time"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/audit"
	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
)

type serverAdapter interface {
	// Permissions are the levels of the players online and of the
	// commands, which Register installs.
	cmd.Permissions
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
	// PlayerIsOperator reports whether the player online of the UUID
	// passed is an operator.
	PlayerIsOperator(id uuid.UUID) bool
	Op(name string) (bool, error)
	Deop(name string) (bool, error)
	// OperatorLevel returns the level of the operator of the name passed,
	// and reports whether it is one; OpLevel the level Op gives.
	OperatorLevel(name string) (cmd.Level, bool)
	OpLevel() cmd.Level
	// SetOperatorLevel makes the player an operator of the level passed.
	SetOperatorLevel(name string, level cmd.Level) (bool, error)
	// ReloadPermissions reads the lists of permissions again.
	ReloadPermissions() error
	OperatorEntries() ([]string, error)
	// OperatorList returns the operators with their levels; RoleEntries
	// the roles of permissions.json as the commands show them.
	OperatorList() ([]server.OperatorInfo, error)
	RoleEntries() ([]string, error)
	// SetRole gives a player a role of permissions.json, and ClearRole
	// takes its entry off it.
	SetRole(name string, role client.Role, abilities client.Abilities) (bool, error)
	ClearRole(name string) (bool, error)
	BanPlayer(name, reason string) (bool, error)
	// Ban and BanAddress ban a player and an address, recording when, by
	// whom and until when; ActorOf is who a source is, as a ban records it.
	Ban(name string, o server.BanOptions) (server.BanChange, error)
	BanAddress(ip string, o server.BanOptions) (server.BanChange, error)
	// BanList returns the bans of players, or of addresses if ips; BanOf
	// the ban of a player, or of an address if ip.
	BanList(ips bool) ([]server.BanInfo, error)
	BanOf(target string, ip bool) (server.BanInfo, bool)
	// BanTarget is the address or network a ban of an address, a network
	// or a player online is kept by; PlayersAt the players online it
	// covers.
	BanTarget(target string) (string, error)
	PlayersAt(key string) []string
	ActorOf(src cmd.Source) audit.Actor
	PardonPlayer(name string) (bool, error)
	BanIP(ip, reason string) (bool, error)
	PardonIP(ip string) (bool, error)
	BanEntries(ip bool) ([]string, error)
	Banned(addr string, name string) (string, bool)
	// ResolvePlayer and ResolveEntry find the player an admin names, by
	// name or by "xuid:<XUID>", to add to a list and to remove from one.
	ResolvePlayer(s string) (server.Target, error)
	ResolveEntry(list server.List, s string) (server.Target, error)
	// SaveAllFrom saves every dimension and the players in them, at one
	// instant, without waiting for it in tx, the transaction the command
	// runs in; done runs in tx's world once they were saved.
	SaveAllFrom(tx *world.Tx, done func(tx *world.Tx, err error))
	// Backup backs the server up, Backups lists the backups, BackupStatus
	// tells whether one runs, and VerifyBackup verifies one; none must be
	// called in a transaction.
	Backup(ctx context.Context, by server.Actor, reason string) (server.BackupInfo, error)
	Backups() ([]server.BackupInfo, error)
	BackupStatus() server.BackupStatus
	VerifyBackup(name string, full bool) (server.BackupInfo, error)
	// HoldSave, QuerySave and ResumeSave are BDS's save hold, query and
	// resume; ResumeSave must not be called in a transaction.
	HoldSave(by server.Actor) error
	QuerySave() server.SaveHold
	ResumeSave() error
	SaveEnabled(enabled bool)
	SaveEnabledState() bool
	SetIdleTimeout(minutes int)
	IdleTimeout() int
	Version() string
}
