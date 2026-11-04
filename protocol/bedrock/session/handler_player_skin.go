package session

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
)

// PlayerSkinHandler handles the PlayerSkin packet.
type PlayerSkinHandler struct{}

// Handle ...
func (PlayerSkinHandler) Handle(p proto.Packet, s *Session, _ *world.Tx, c Controllable) error {
	pk := p.(*mc.PlayerSkinPacket)
	sk := protocolToSkin(pk.Skin)
	if s.tables().proto.Number >= protocol2225 {
		// A client of 2225 sends no PlayFab ID in its skin: the player keeps
		// its own, which the player list tells.
		sk.PlayFabID = c.Skin().PlayFabID
	}
	c.SetSkin(sk)
	return nil
}
