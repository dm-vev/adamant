package session

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
)

// containerTypeNone is the type of the ContainerClose BDS answers with when
// the window it closes has none: the inventory, and nothing at all.
const containerTypeNone = 247

// anyWindow is the window the client names when it closes whatever it shows,
// as it does when it opens the inventory at the same time as chat.
const anyWindow = 0xff

// ContainerCloseHandler handles the ContainerClose packet.
type ContainerCloseHandler struct{}

// Handle ...
func (h *ContainerCloseHandler) Handle(p proto.Packet, s *Session, tx *world.Tx, c Controllable) error {
	pk := p.(*mc.ContainerClosePacket)

	c.MoveItemsToInventory()

	answer := mc.ContainerClosePacket{WindowID: pk.WindowID}
	switch pk.WindowID {
	case 0:
		// Closing of the normal inventory.
		s.windows.inventory = false
		if s.menu.Load() != nil {
			// The client shows no menu if it closes the inventory.
			s.closeUnknown(c, 0)
		}
	case byte(s.openedWindowID.Load()):
		answer.ContainerType = byte(s.openedContainerID.Load())
		c.CloseMenu(true)
	case anyWindow:
		answer.WindowID, answer.ContainerType = s.closeWhatIsOpen(c)
	default:
		answer.ContainerType = pk.ContainerType
		s.closeUnknown(c, pk.WindowID)
	}
	// Every close is answered, also one the client flags as server side, as
	// BDS answers any close and PocketMine-MP answers "even if no window
	// matches": a client told a window closed behaves as if it closed it, and
	// waits for the answer. An answer is not server side, so a client does not
	// answer it in turn.
	s.writePacket(&answer)
	return nil
}

// closeWhatIsOpen closes whatever the client has open for its close of any
// window, as BDS does, and returns the window and type that the ContainerClose
// it is answered with names: the menu, the inventory (0), or, with nothing
// open, the last window opened.
//
// The grace of windows.go does not apply. It guards against a close of a
// window the session does not show, which crossed the ContainerOpen of the
// one it does, and so must not end that one. The 0xff names no window, and the
// answer names the window the session has open, after its ContainerOpen on the
// same connection: a 0xff that crossed the open of a chest ends in the client
// being shown the chest and then its close, never a chest the session closed
// and the client still shows.
func (s *Session) closeWhatIsOpen(c Controllable) (window, containerType byte) {
	if s.menu.Load() != nil {
		window, containerType = byte(s.openedWindowID.Load()), byte(s.openedContainerID.Load())
		c.CloseMenu(true)
		return window, containerType
	}
	if s.windows.inventory {
		s.windows.inventory = false
		return 0, containerTypeNone
	}
	return byte(s.openedWindowID.Load()), containerTypeNone
}
