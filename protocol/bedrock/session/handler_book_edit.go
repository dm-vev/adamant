package session

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// BookEditHandler handles the BookEdit packet.
type BookEditHandler struct{}

// Handle ...
func (b BookEditHandler) Handle(p proto.Packet, _ *Session, _ *world.Tx, c Controllable) error {
	pk := p.(*v2193.BookEditPacket)
	return c.EditBook(int(pk.InventorySlot), item.BookEdit{
		Action: bookAction(pk.ActionType),
		Page:   int(pk.PageNumber),
		Other:  int(pk.SecondaryPageNumber),
		Text:   pk.Text,
		Title:  pk.Title,
	})
}

// bookAction returns the item.BookAction of the action type of a BookEdit
// packet. An unknown type is the zero BookAction, which writes the book back
// unchanged.
func bookAction(t uint32) item.BookAction {
	switch t {
	case v2193.BookActionReplacePage:
		return item.BookReplacePage
	case v2193.BookActionAddPage:
		return item.BookAddPage
	case v2193.BookActionDeletePage:
		return item.BookDeletePage
	case v2193.BookActionSwapPages:
		return item.BookSwapPages
	case v2193.BookActionSign:
		return item.BookSign
	}
	return 0
}
