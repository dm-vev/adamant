package session

import (
	"encoding/json"
	"net"

	"github.com/dm-vev/adamant/net/bedrock/proto"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	bedrocktext "github.com/dm-vev/adamant/protocol/bedrock/text"
	"github.com/dm-vev/adamant/server/player/dialogue"
	"github.com/dm-vev/adamant/server/player/form"
	"github.com/dm-vev/adamant/server/text"
	"github.com/dm-vev/adamant/server/world"
)

// Disconnect disconnects the client and ultimately closes the session. The message passed is shown to the
// client as the reason, written with bedrocktext.Inline, so that a translation without arguments is
// translated by the client; a message that renders empty disconnects the client with no screen.
func (s *Session) Disconnect(message text.Text) {
	if s != nop {
		msg := bedrocktext.Inline(message)
		var pk proto.Packet = &mc.DisconnectPacket{
			HideDisconnectionScreen: msg == "",
			Message:                 msg,
		}
		if s.hasOutInterceptors.Load() {
			var ok bool
			if pk, ok = s.runOutInterceptors(pk); !ok || pk == nil {
				return
			}
		}
		s.connWriteMu.Lock()
		_ = s.conn.WritePacket(pk)
		_ = s.conn.Flush()
		s.connWriteMu.Unlock()
	}
}

// SendDialogue sends an NPC dialogue to the client of the connection. The Submit method of the dialogue is
// called when the client interacts with a button in the dialogue.
func (s *Session) SendDialogue(d dialogue.Dialogue, e world.Entity) {
	// nop sessions are returned for offline players and must remain safe to call.
	if s == nop || s.handlers == nil {
		return
	}
	if e == nil {
		if s.conf.Log != nil {
			s.conf.Log.Error("SendDialogue: nil entity")
		}
		return
	}
	b, err := json.Marshal(d)
	if err != nil {
		if s.conf.Log != nil {
			s.conf.Log.Error("SendDialogue: failed to marshal dialogue", "err", err)
		}
		return
	}

	h, ok := s.handlers[mc.IDNPCRequest].(*NPCRequestHandler)
	if !ok || h == nil {
		if s.conf.Log != nil {
			s.conf.Log.Error("SendDialogue: missing NPC request handler")
		}
		return
	}
	entityRuntimeID := s.handleRuntimeID(e.H())
	if entityRuntimeID == 0 {
		return
	}
	dialogueCopy := d
	h.mu.Lock()
	h.dialogue = &dialogueCopy
	h.entityRuntimeID = entityRuntimeID
	h.entityUniqueID = uint64(s.entityUniqueID(e))
	h.mu.Unlock()

	metadata := s.parseEntityMetadata(e)
	metadata[mc.EntityDataKeyHasNPC] = uint8(1)

	disp := d.Display()
	disp.EntityOffset = disp.EntityOffset.Add(entityOffset(e))
	display, err := json.Marshal(map[string]any{"portrait_offsets": disp})
	if err != nil {
		if s.conf.Log != nil {
			s.conf.Log.Error("SendDialogue: failed to marshal dialogue display", "err", err)
		}
	} else {
		metadata[mc.EntityDataKeyNPCData] = string(display)
	}

	s.writePacket(&mc.SetActorDataPacket{
		EntityRuntimeID: entityRuntimeID,
		EntityMetadata:  toMetadata(metadata),
	})
	s.writePacket(&mc.NPCDialoguePacket{
		EntityUniqueID: uint64(s.entityUniqueID(e)),
		ActionType:     mc.NPCDialogueActionOpen,
		Dialogue:       d.Body(),
		SceneName:      "default",
		NPCName:        d.Title(),
		ActionJSON:     string(b),
	})
}

func (s *Session) CloseDialogue() {
	if s == nop || s.handlers == nil {
		return
	}
	h, ok := s.handlers[mc.IDNPCRequest].(*NPCRequestHandler)
	if !ok || h == nil {
		if s.conf.Log != nil {
			s.conf.Log.Error("CloseDialogue: missing NPC request handler")
		}
		return
	}
	h.mu.Lock()
	entityRuntimeID := h.entityRuntimeID
	entityUniqueID := h.entityUniqueID
	h.entityRuntimeID = 0
	h.entityUniqueID = 0
	h.dialogue = nil
	h.mu.Unlock()
	if entityRuntimeID == 0 {
		return
	}

	s.writePacket(&mc.NPCDialoguePacket{
		EntityUniqueID: entityUniqueID,
		ActionType:     mc.NPCDialogueActionClose,
	})
}

// SendForm sends a form to the client of the connection. The Submit method of the form is called when the
// client submits the form.
func (s *Session) SendForm(f form.Form) {
	if s == nop || s.handlers == nil {
		return
	}
	b, err := json.Marshal(f)
	if err != nil {
		if s.conf.Log != nil {
			s.conf.Log.Error("SendForm: failed to marshal form", "err", err)
		}
		return
	}

	h, ok := s.handlers[mc.IDModalFormResponse].(*ModalFormResponseHandler)
	if !ok || h == nil {
		if s.conf.Log != nil {
			s.conf.Log.Error("SendForm: missing modal form handler")
		}
		return
	}
	id := h.currentID.Add(1)

	h.mu.Lock()
	if len(h.forms) > 10 {
		s.conf.Log.Debug("SendForm: more than 10 active forms: dropping an existing one")
		for k := range h.forms {
			delete(h.forms, k)
			break
		}
	}
	h.forms[id] = f
	h.mu.Unlock()

	s.writePacket(&mc.ModalFormRequestPacket{
		FormID:   id,
		FormData: b,
	})
}

// CloseForm closes any forms that the player currently has open. If the player has no forms open, nothing
// happens.
func (s *Session) CloseForm() {
	s.writePacket(&mc.ClientBoundCloseFormPacket{})
}

// Transfer transfers the player to a server with the IP and port passed.
func (s *Session) Transfer(ip net.IP, port int) {
	if port < 0 || port > 65535 {
		if s.conf.Log != nil {
			s.conf.Log.Error("Transfer: invalid port", "port", port)
		}
		return
	}
	s.writePacket(&mc.TransferPacket{
		Address: ip.String(),
		Port:    uint16(port),
	})
}
