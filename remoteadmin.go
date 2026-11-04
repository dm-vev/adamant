package main

import (
	"github.com/df-mc/dragonfly/server/logging"
	"github.com/df-mc/dragonfly/server/remoteadmin"
)

// remoteAdminService returns the remote admin API as [RemoteAdmin] says,
// on a listener of its own, apart from the monitor, as a service of the
// server: it serves as the server starts, and stops as it closes. A
// listener that cannot start is logged, and the server runs without it, as
// without its monitor.
func remoteAdminService(proc process) *remoteadmin.Service {
	return proc.remote.Service(remoteadmin.ListenOptions{Dir: proc.adminDir, Log: proc.logs.Logger(logging.Server)})
}
