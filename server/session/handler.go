package session

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
)

// packetHandler represents a type that is able to handle a specific type of incoming packets from the client.
type packetHandler interface {
	// Handle handles an incoming packet from the client. The session of the client is passed to the packetHandler.
	// Handle returns an error if the packet was in any way invalid.
	Handle(p proto.Packet, s *Session, tx *world.Tx, c Controllable) error
}
