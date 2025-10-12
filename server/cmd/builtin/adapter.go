package builtin

import (
	"iter"

	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

type serverAdapter interface {
	Players(tx *world.Tx) iter.Seq[*player.Player]
	MaxPlayerCount() int
	Close() error
}
