package server

import (
	"github.com/df-mc/dragonfly/server/world"
)

// Status is what the server shows of itself in the server lists of clients,
// before they join.
type Status struct {
	// Name is the name of the server, and SubName the line some lists show
	// below it.
	Name, SubName string
	// PlayerCount is the number of players online, and MaxPlayers the most
	// that may be.
	PlayerCount, MaxPlayers int
}

// StatusProvider gives the status a server shows in server lists. Set as
// Config.StatusProvider, it replaces the default, which shows Config.Name.
type StatusProvider interface {
	// ServerStatus returns the status of the server when playerCount players
	// are online of at most maxPlayers.
	ServerStatus(playerCount, maxPlayers int) Status
}

// statusProvider handles the way the server shows up in the server list. The
// online players and maximum players are not changeable from outside the
// server, but the server name may be changed at any time.
type statusProvider struct {
	name string
}

// ServerStatus returns the player count, max players and the server's name as
// a Status.
func (s statusProvider) ServerStatus(playerCount, maxPlayers int) Status {
	return Status{
		Name:        s.name,
		PlayerCount: playerCount,
		MaxPlayers:  maxPlayers,
	}
}

// Host is the server as its listeners see it: what they show the clients
// that look the server up before they join. *Server implements it.
type Host interface {
	// Status returns the status of the server now: what Config.StatusProvider
	// gives for PlayerCount() and MaxPlayerCount().
	Status() Status
	// PlayerNames returns the names of the players online, in no order.
	PlayerNames() []string
	// World returns the default world of the server; nil while New is still
	// making it.
	World() *world.World
	// WhitelistEnabled reports whether the whitelist is enforced.
	WhitelistEnabled() bool
	// GameVersions returns the version of the game the server speaks to its
	// clients and the versions of the clients that may join, as
	// Server.GameVersions does.
	GameVersions() (primary string, joinable []string)
}

// Status returns the status of the server now: what Config.StatusProvider
// gives for PlayerCount and MaxPlayerCount.
func (srv *Server) Status() Status {
	return srv.conf.StatusProvider.ServerStatus(srv.PlayerCount(), srv.MaxPlayerCount())
}

// PlayerNames returns the names of the players online, in no order.
func (srv *Server) PlayerNames() []string {
	return srv.players.names()
}
