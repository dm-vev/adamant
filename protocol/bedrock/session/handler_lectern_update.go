package session

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// LecternUpdateHandler handles the LecternUpdate packet, sent when a player interacts with a lectern.
type LecternUpdateHandler struct{}

// Handle ...
func (LecternUpdateHandler) Handle(p proto.Packet, _ *Session, tx *world.Tx, c Controllable) error {
	pk := p.(*v2193.LecternUpdatePacket)
	pos := blockPosFromProtocol(pk.Position)
	if !canReach(c, pos.Vec3Middle()) {
		return fmt.Errorf("block at %v is not within reach", pos)
	}
	if _, ok := tx.Block(pos).(block.Lectern); !ok {
		return fmt.Errorf("block at %v is not a lectern", pos)
	}
	return c.TurnLecternPage(pos, int(pk.Page))
}
