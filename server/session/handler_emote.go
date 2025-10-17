package session

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	"time"
)

// EmoteHandler handles the Emote packet.
type EmoteHandler struct {
	LastEmote time.Time
}

// Handle ...
func (h *EmoteHandler) Handle(p packet.Packet, _ *Session, tx *world.Tx, c Controllable) error {
	pk := p.(*packet.Emote)

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
	viewers := tx.Viewers(c.Position())
	// Emotes are cosmetic but frequent; reusing the viewer slice prevents busy lobbies from generating a stream of
	// temporary allocations as players spam emotes.
	for _, viewer := range viewers {
		viewer.ViewEmote(c, emote)
	}
	tx.ReleaseViewers(viewers)
	return nil
}
