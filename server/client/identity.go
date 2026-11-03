package client

import "github.com/google/uuid"

// Identity is who a client logged in as: the player it plays. The server
// keys the data of players and reconnects by UUID, and the whitelist, the
// operators and the bans by Name, trimmed and lower-cased. Platform IDs,
// such as the XUID, are optional attributes beside them, empty for a client
// that logged in without the platform.
type Identity struct {
	// Name is the name of the player as it logged in: its display name on
	// Bedrock Edition. It does not change while the player plays, whatever
	// name tag it is given.
	Name string
	// UUID identifies the player across its logins. It is not uuid.Nil in
	// an Identity a Listener yields.
	UUID uuid.UUID
	// XUID is the Xbox Live user ID of a player that logged in with Xbox
	// Live, in decimal; empty otherwise.
	XUID string
}
