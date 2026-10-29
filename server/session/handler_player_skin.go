package session

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// PlayerSkinHandler handles the PlayerSkin packet.
type PlayerSkinHandler struct{}

// Handle ...
func (PlayerSkinHandler) Handle(p proto.Packet, _ *Session, _ *world.Tx, c Controllable) error {
	pk := p.(*v2193.PlayerSkinPacket)
	c.SetSkin(protocolToSkin(pk.Skin))
	return nil
}
