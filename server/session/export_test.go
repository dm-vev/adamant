package session

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
)

// HandlePacket handles a packet as the session does once it read one, for
// the tests of other packages, without the recovery of the goroutine that
// reads: a panic of a handler reaches the test.
func (s *Session) HandlePacket(pk proto.Packet, tx *world.Tx, c Controllable) error {
	return s.handlePacket(pk, tx, c)
}
