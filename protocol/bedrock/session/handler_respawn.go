package session

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// RespawnHandler handles the Respawn packet.
type RespawnHandler struct{}

// Handle ...
func (*RespawnHandler) Handle(p proto.Packet, _ *Session, _ *world.Tx, c Controllable) error {
	pk := p.(*v2193.RespawnPacket)
	if pk.EntityRuntimeID != selfEntityRuntimeID {
		return errSelfRuntimeID
	}
	if pk.State != v2193.RespawnStateClientReadyToSpawn {
		return fmt.Errorf("respawn state must always be %v, but got %v", v2193.RespawnStateClientReadyToSpawn, pk.State)
	}
	c.Respawn()
	return nil
}
