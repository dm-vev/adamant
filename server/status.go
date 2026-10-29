package server

import (
	"github.com/df-mc/dragonfly/server/netconn"
)

// statusProvider handles the way the server shows up in the server list. The
// online players and maximum players are not changeable from outside the
// server, but the server name may be changed at any time.
type statusProvider struct {
	name string
}

// ServerStatus returns the player count, max players and the server's name as
// a netconn.ServerStatus.
func (s statusProvider) ServerStatus(playerCount, maxPlayers int) netconn.ServerStatus {
	return netconn.ServerStatus{
		ServerName:  s.name,
		PlayerCount: playerCount,
		MaxPlayers:  maxPlayers,
	}
}
