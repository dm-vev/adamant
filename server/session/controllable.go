package session

import "github.com/df-mc/dragonfly/server/client"

// Controllable is the player a Session drives: client.Controllable, named
// here so that the handlers of the session keep their signatures.
type Controllable = client.Controllable

// Diagnostics are the latest diagnostics a client sent: client.Diagnostics,
// named here so that handlers that name the session's type keep compiling.
type Diagnostics = client.Diagnostics

// Session is the client of a player that plays over Bedrock Edition.
var _ client.Joining = (*Session)(nil)
