package server

import (
	"net"

	"github.com/df-mc/dragonfly/server/client"
)

// Allower may be implemented to specifically allow or disallow players from
// joining a Server, by setting it as Config.Allower.
type Allower interface {
	// Allow filters which clients may join the Server. It is passed the
	// address, the identity and the profile of the client. If Allow returns
	// false, the client is disconnected with the string returned as the
	// message, a legacy string with § codes in their Bedrock Edition
	// meanings. WARNING: use the profile at your own risk: the client tells
	// it, and may tell anything.
	Allow(addr net.Addr, id client.Identity, p client.Profile) (string, bool)
}

// allower is the standard Allower implementation. It accepts all connections.
type allower struct{}

// Allow always returns true.
func (allower) Allow(net.Addr, client.Identity, client.Profile) (string, bool) {
	return "", true
}
