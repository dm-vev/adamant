package session

import (
	"time"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/google/uuid"
)

// EmoteHandler handles the Emote packet.
type EmoteHandler struct {
	LastEmote time.Time
}

// Handle ...
func (h *EmoteHandler) Handle(p proto.Packet, _ *Session, _ *world.Tx, c Controllable) error {
	pk := p.(*v2193.EmotePacket)

	if pk.EntityRuntimeID != selfEntityRuntimeID {
		return errSelfRuntimeID
	}
	if time.Since(h.LastEmote) < time.Second {
		return nil
	}
	h.LastEmote = time.Now()
	emote, err := uuid.Parse(pk.EmoteID)
	if err != nil {
		return err
	}
	c.Emote(emote)
	return nil
}
