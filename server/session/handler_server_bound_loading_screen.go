package session

import (
	"fmt"
	"sync/atomic"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// ServerBoundLoadingScreenHandler handles loading screen updates from the clients. It is used to ensure that
// the server knows when the client is loading a screen, and when it is done loading it.
type ServerBoundLoadingScreenHandler struct {
	currentID  atomic.Uint32
	expectedID atomic.Uint32
}

// Handle ...
func (h *ServerBoundLoadingScreenHandler) Handle(p proto.Packet, s *Session, _ *world.Tx, _ Controllable) error {
	pk := p.(*v2193.ServerBoundLoadingScreenPacket)
	v, ok := pk.LoadingScreenID.Value()
	expected := h.expectedID.Load()

	switch {
	case !ok || expected == 0:
		return nil
	case int32(v-expected) < 0:
		// A later transfer may supersede this screen before its ACK arrives.
		// Compare serial numbers across wraparound; never clear the newer one.
		return nil
	case v != expected:
		return fmt.Errorf("expected loading screen ID %d, got %d", expected, v)
	case pk.Type == packet.LoadingScreenTypeEnd:
		s.changingDimension.Store(false)
		h.expectedID.Store(0)
	}

	return nil
}
