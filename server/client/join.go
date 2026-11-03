package client

import (
	"context"
	"net"

	"github.com/df-mc/dragonfly/server/player/skin"
	"github.com/df-mc/dragonfly/server/text"
	"github.com/df-mc/dragonfly/server/world"
	"golang.org/x/text/language"
)

// Login is what a client told of itself as it logged in.
type Login struct {
	Identity Identity
	Profile  Profile
	// Locale is the language the client plays in, as its adapter read it.
	Locale language.Tag
	// Skin is the skin the client joins with.
	Skin skin.Skin
	// Addr is the address of the client.
	Addr net.Addr
}

// Join is the request of a client to join the server as a player. The
// client logged in, and its adapter admitted it: it held the client to the
// login policy and the Allower of the server before yielding the Join.
//
// The server answers a Join once, with Start or with Reject; Reject may
// also follow a Start that failed. After a Start that succeeded, the client
// is the Joining that Start returned. The server calls a Join on a
// goroutine of its own, never on the goroutine of a world.
type Join interface {
	// Login returns what the client told of itself. The first call may be
	// costly, as the adapter reads the skin then; the server calls it once.
	Login() Login
	// Start starts the game for the client as g says. The client is shown
	// the world and the settings of the server, and the content of the
	// game is defined to it. Start returns the client of the player once
	// the client is ready for the player to spawn. It returns an error if
	// the client went away, or if ctx was done first; the client is then
	// still connected, and the server rejects it.
	Start(ctx context.Context, g Game) (Joining, error)
	// Reject refuses the client: it is shown reason as the reason it was
	// disconnected, and disconnected. A reason that renders empty
	// disconnects it with no screen.
	Reject(reason text.Text)
}

// Joining is the client of a player that is joining: its game started,
// and the server makes the player, binds the client to it and spawns it.
type Joining interface {
	Client
	// Bind binds the client to h, the entity handle of its player, which
	// joins with the skin sk. The server calls it once, before it adds the
	// player to a world, on no world's goroutine.
	Bind(h *world.EntityHandle, sk skin.Skin)
	// Spawn shows the client its player c, which the server just added to
	// the world of tx, and the world around it. It subscribes c to the chat,
	// writes the JoinMessage of the Game, and starts handing c what the
	// client sends. The server calls it once, in tx.
	Spawn(c Controllable, tx *world.Tx)
}
