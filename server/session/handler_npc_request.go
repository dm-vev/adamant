package session

import (
	"fmt"
	"sync"

	"github.com/df-mc/dragonfly/server/player/dialogue"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// NPCRequestHandler handles the NPCRequest packet.
type NPCRequestHandler struct {
	// mu guards dialogue state accessed by network handlers and gameplay goroutines.
	mu sync.Mutex

	dialogue        *dialogue.Dialogue
	entityRuntimeID uint64
}

// Handle ...
func (h *NPCRequestHandler) Handle(p packet.Packet, s *Session, tx *world.Tx, c Controllable) error {
	pk := p.(*packet.NPCRequest)
	h.mu.Lock()
	dialoguePtr := h.dialogue
	h.mu.Unlock()
	if dialoguePtr == nil {
		// Dialogue was closed or replaced before the response arrived.
		return nil
	}
	dialogue := *dialoguePtr

	if pk.RequestType == packet.NPCRequestActionExecuteAction {
		if err := dialogue.Submit(uint(pk.ActionType), c, tx); err != nil {
			return fmt.Errorf("error submitting dialogue: %w", err)
		}
	} else if pk.RequestType == packet.NPCRequestActionExecuteClosingCommands {
		dialogue.Close(c, tx)
		h.mu.Lock()
		if h.dialogue == dialoguePtr {
			h.dialogue = nil
			h.entityRuntimeID = 0
		}
		h.mu.Unlock()
	}
	return nil
}
