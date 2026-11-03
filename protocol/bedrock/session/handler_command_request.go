package session

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// CommandRequestHandler handles the CommandRequest packet.
type CommandRequestHandler struct{}

// Handle ...
func (h *CommandRequestHandler) Handle(p proto.Packet, s *Session, _ *world.Tx, c Controllable) error {
	pk := p.(*v2193.CommandRequestPacket)
	if pk.Internal {
		return fmt.Errorf("command request packet must never have the internal field set to true")
	}

	if len(pk.CommandLine) > maxNBTString {
		return fmt.Errorf("command line over %d bytes", maxNBTString)
	}
	s.setCommandOrigin(pk.CommandOrigin)
	c.ExecuteCommand(pk.CommandLine)
	return nil
}
