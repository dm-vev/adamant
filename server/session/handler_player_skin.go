package session

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// PlayerSkinHandler handles the PlayerSkin packet.
type PlayerSkinHandler struct{}

// Handle ...
func (PlayerSkinHandler) Handle(p proto.Packet, _ *Session, _ *world.Tx, c Controllable) error {
	pk := p.(*v2193.PlayerSkinPacket)

	playerSkin, err := protocolToSkin(pk.Skin)
	if err != nil {
		return fmt.Errorf("error decoding skin: %w", err)
	}

	c.SetSkin(playerSkin)

	return nil
}
