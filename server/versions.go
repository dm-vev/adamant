package server

import (
	"fmt"
	"strings"

	"github.com/df-mc/dragonfly/server/version"
)

// gameVersioner is implemented by a Listener that knows which versions of
// the game its clients join with. The server cannot ask the protocols
// themselves: the adapters that know them are above it.
type gameVersioner interface {
	// GameVersions returns the version of the game the listener speaks to
	// its clients, its primary, and the versions of the clients that may
	// join, oldest first, each as a version or a range such as
	// "26.50–26.52".
	GameVersions() (primary string, joinable []string)
}

// GameVersions returns the version of the game the server speaks to its
// clients and the versions of the clients that may join, as the first
// listener that knows them reports. A server with no such listener speaks the
// release it implements, version.Minecraft, and lists no other.
func (srv *Server) GameVersions() (primary string, joinable []string) {
	for _, l := range srv.listeners {
		if g, ok := l.(gameVersioner); ok {
			return g.GameVersions()
		}
	}
	return version.Minecraft, nil
}

// versionDescription returns the versions of the game the server speaks and
// accepts, as the version it speaks and the clients that may join
// after a semicolon, such as "1.26.60; clients 26.50–26.52, 26.60–26.61".
func (srv *Server) versionDescription() string {
	primary, joinable := srv.GameVersions()
	if len(joinable) == 0 {
		return primary
	}
	return fmt.Sprintf("%s; clients %s", primary, strings.Join(joinable, ", "))
}
