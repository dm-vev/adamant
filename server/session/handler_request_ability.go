package session

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// RequestAbilityHandler handles the RequestAbility packet.
type RequestAbilityHandler struct{}

// Handle ...
func (a RequestAbilityHandler) Handle(p proto.Packet, s *Session, _ *world.Tx, c Controllable) error {
	pk := p.(*v2193.RequestAbilityPacket)
	if pk.Ability == v2193.AbilityFlyingIndex {
		if !c.GameMode().AllowsFlying() {
			s.conf.Log.Debug("process packet: RequestAbility: flying flag enabled while unable to fly")
			s.SendAbilities(c)
			return nil
		}
		c.StartFlying()
	}
	return nil
}
