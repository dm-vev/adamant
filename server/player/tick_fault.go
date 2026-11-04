package player

import (
	"github.com/df-mc/dragonfly/server/text"
	"github.com/df-mc/dragonfly/server/world"
)

// TickPanicked disconnects the player with an internal error, as D2 chose
// for a panic of its tick or of its controller: such a player is left in a
// state that cannot be trusted, and freezing it would leave its client
// connected to nothing. Leaving saves it. The world logged the panic.
func (p *Player) TickPanicked(*world.Tx, world.Fault) {
	p.Disconnect(text.Literal("Internal server error"))
}

var _ world.TickFaulter = (*Player)(nil)
