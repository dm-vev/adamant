package server

import (
	"io"

	"github.com/df-mc/dragonfly/server/client"
)

// Listener is a source of clients, of some protocol, that ask to join the
// server. The Server accepts their Joins from it and answers each one.
// Proxies can use this to provide players from a different source.
//
// A Listener admits a client before it yields its Join, at the point of
// the login of its protocol where that fits. It holds the identity of the
// client to the login policy of the server, asks the Allower of the
// Config it was made with, and refuses the client with the message of the
// Allower.
type Listener interface {
	// Accept blocks until the next client asks to join and returns its
	// Join. An error is returned once the Listener was closed using Close.
	Accept() (client.Join, error)
	io.Closer
}
