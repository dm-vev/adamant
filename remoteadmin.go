package main

import (
	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/logging"
	"github.com/df-mc/dragonfly/server/remoteadmin"
)

// serveRemoteAdmin serves the remote admin API of srv as [RemoteAdmin]
// says, on a listener of its own, apart from the monitor, and returns it,
// nil if the API is off or cannot be served. A listener that cannot start
// is logged, and the server runs without it, as without its monitor.
func serveRemoteAdmin(proc process, srv *server.Server) *remoteadmin.Listener {
	log := proc.logs.Logger(logging.Server)
	l, err := proc.remote.Listen(srv, remoteadmin.ListenOptions{Dir: proc.adminDir, Log: log})
	if err != nil {
		log.Error("Remote admin not served: " + err.Error())
		return nil
	}
	return l
}
