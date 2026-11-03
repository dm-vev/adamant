package session

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// LecternUpdateHandler handles the LecternUpdate packet, sent when a player interacts with a lectern.
type LecternUpdateHandler struct{}

// Handle ...
func (LecternUpdateHandler) Handle(p proto.Packet, _ *Session, _ *world.Tx, c Controllable) error {
	pk := p.(*v2193.LecternUpdatePacket)
	// The player checks the reach and the lectern.
	return c.TurnLecternPage(blockPosFromProtocol(pk.Position), int(pk.Page))
}
