package session

import (
	"fmt"

	"github.com/dm-vev/adamant/net/bedrock/proto"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/dm-vev/adamant/server/item"
	"github.com/dm-vev/adamant/server/world"
)

// TextHandler handles the Text packet.
type TextHandler struct{}

// Handle ...
func (TextHandler) Handle(p proto.Packet, s *Session, _ *world.Tx, c Controllable) error {
	pk := p.(*mc.TextPacket)

	if pk.TextType != mc.TextTypeChat {
		return fmt.Errorf("TextType should always be Chat (%v), but got %v", mc.TextTypeChat, pk.TextType)
	}
	if pk.SourceName != s.conn.IdentityData().DisplayName {
		return fmt.Errorf("SourceName must be equal to DisplayName")
	}
	if pk.XUID != s.conn.IdentityData().XUID {
		return fmt.Errorf("XUID must be equal to player's XUID")
	}
	if !item.ValidText(pk.Message, maxChatLength) {
		// As BDS does, a message too long to pass on is dropped.
		return nil
	}
	c.Chat(pk.Message)
	return nil
}
