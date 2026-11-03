package session

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// BlockPickRequestHandler handles the BlockPickRequest packet.
type BlockPickRequestHandler struct{}

// Handle ...
func (b BlockPickRequestHandler) Handle(p proto.Packet, _ *Session, _ *world.Tx, c Controllable) error {
	pk := p.(*v2193.BlockPickRequestPacket)
	c.PickBlock(cube.Pos{int(pk.Position[0]), int(pk.Position[1]), int(pk.Position[2])})
	return nil
}
