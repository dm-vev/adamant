package client

import "github.com/google/uuid"

// Identity is who a client logged in as: the player it plays. What the
// server keys its players by, the XUID or the name, is its identity.Mode.
type Identity struct {
	// Name is the name of the player as it logged in: its display name on
	// Bedrock Edition. It does not change while the player plays, whatever
	// name tag it is given.
	Name string
	// UUID identifies the player across its logins, as its client sent it.
	// It is not uuid.Nil in an Identity a Listener yields. Unless
	// Authenticated, the client chose it, and may have chosen any.
	UUID uuid.UUID
	// XUID is the Xbox Live user ID the login names, in decimal; empty if
	// it names none. Unless Authenticated, the client claimed it.
	XUID string
	// Authenticated reports that a service the server trusts vouched for
	// the identity: Xbox Live signed it, or a proxy holding the secret of
	// the server's links says Xbox Live did. Otherwise the client named
	// itself, and may have named itself anything.
	Authenticated bool
}
