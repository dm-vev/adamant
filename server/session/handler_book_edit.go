package session

import (
	"fmt"
	"unicode/utf8"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// BookEditHandler handles the BookEdit packet.
type BookEditHandler struct{}

// Handle ...
func (b BookEditHandler) Handle(p proto.Packet, s *Session, _ *world.Tx, c Controllable) error {
	pk := p.(*v2193.BookEditPacket)

	it, err := s.inv.Item(int(pk.InventorySlot))
	if err != nil {
		return fmt.Errorf("invalid inventory slot index %v", pk.InventorySlot)
	}
	book, ok := it.Item().(item.BookAndQuill)
	if !ok {
		return fmt.Errorf("inventory slot %v does not contain a writable book", pk.InventorySlot)
	}

	page := int(pk.PageNumber)
	if page >= 50 || page < 0 {
		return fmt.Errorf("page number %v is out of bounds", pk.PageNumber)
	}
	if len(pk.Text) > 256 || !utf8.ValidString(pk.Text) {
		return fmt.Errorf("text must be UTF-8 of at most 256 bytes")
	}

	slot := int(pk.InventorySlot)
	switch pk.ActionType {
	case packet.BookActionReplacePage:
		book = book.SetPage(page, pk.Text)
	case packet.BookActionAddPage:
		if len(book.Pages) >= 50 {
			return fmt.Errorf("unable to add page beyond 50")
		}
		if page >= len(book.Pages) && page <= len(book.Pages)+2 {
			book = book.SetPage(page, "")
			break
		}
		if _, ok := book.Page(page); !ok {
			return fmt.Errorf("unable to insert page at %v", pk.PageNumber)
		}
		book = book.InsertPage(page, pk.Text)
	case packet.BookActionDeletePage:
		if _, ok := book.Page(page); !ok {
			// We break here instead of returning an error because the client can be a page or two ahead in the UI then
			// the actual pages representation server side. The client still sends the deletion indexes.
			break
		}
		book = book.DeletePage(page)
	case packet.BookActionSwapPages:
		if pk.SecondaryPageNumber >= 50 {
			return fmt.Errorf("page number out of bounds")
		}
		_, ok := book.Page(page)
		_, ok2 := book.Page(int(pk.SecondaryPageNumber))
		if !ok || !ok2 {
			// We break here instead of returning an error because the client can try to swap pages that don't exist.
			// This happens as a result of the client being a page or two ahead in the UI then the actual pages
			// representation server side. The client still sends the swap indexes.
			break
		}
		book = book.SwapPages(page, int(pk.SecondaryPageNumber))
	case packet.BookActionSign:
		if !validText(pk.Title, maxBookTitle) {
			return fmt.Errorf("title must be UTF-8 of at most %d characters", maxBookTitle)
		}
		// The author is the player who signs, whoever the client names.
		_ = s.inv.SetItem(slot, it.WithItem(item.WrittenBook{Title: pk.Title, Author: c.Name(), Pages: book.Pages, Generation: item.OriginalGeneration()}))
		return nil
	}
	_ = s.inv.SetItem(slot, it.WithItem(book))
	return nil
}
