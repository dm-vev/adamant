package server

// Service is something a Server runs beside its game for as long as it
// runs, such as the remote admin API or an HTTP server of metrics: the
// Server starts it as it starts, and closes it as it closes, so that a
// program that closes a Server and starts another in the same process
// leaves none of its goroutines, ports or files behind.
type Service interface {
	// Start starts the service for the server passed, in the background,
	// and returns at once. Server.Listen calls it, before the server
	// accepts players. An error is logged, and the server runs without the
	// service, which is not closed.
	Start(srv *Server) error
	// Close stops the service and releases all Start took, once Start
	// returned no error. Server.Close calls it, after the server stopped
	// accepting players and closed its worlds, and before it closes its
	// audit log, which the service may write to until then. It must not
	// wait for a world.
	Close() error
}

// startServices starts the services of the Config, in order, and keeps
// those that started, for closeServices.
func (srv *Server) startServices() {
	for _, s := range srv.conf.Services {
		if s == nil {
			continue
		}
		if err := s.Start(srv); err != nil {
			srv.conf.Log.Error("Service not started: " + err.Error())
			continue
		}
		srv.services = append(srv.services, s)
	}
}

// closeServices closes the services that started, the last started first.
func (srv *Server) closeServices() {
	for i := len(srv.services) - 1; i >= 0; i-- {
		s := srv.services[i]
		if err := s.Close(); err != nil {
			srv.conf.Log.Error("Close a service: " + err.Error())
		}
	}
	srv.services = nil
}
