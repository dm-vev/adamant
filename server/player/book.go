package player

import (
	"fmt"
	"unicode/utf8"

	"github.com/df-mc/dragonfly/server/item"
)

// EditBook applies edit to the book and quill in slot of the inventory of the
// player. It fails if the slot does not hold a book and quill, if the page of
// the edit is not one a book can have, if its text is not UTF-8 of at most
// item.MaxBookPageLength bytes, or if the edit cannot apply to the book: a
// page added beyond the last, or a title that is not UTF-8 of at most
// item.MaxBookTitleLength characters. An edit of pages the book does not have
// leaves it unchanged, as a client a page or two ahead of the book sends one.
// A book signed is written by the player, whoever its client named.
func (p *Player) EditBook(slot int, edit item.BookEdit) error {
	it, err := p.inv.Item(slot)
	if err != nil {
		return fmt.Errorf("invalid inventory slot index %v", slot)
	}
	book, ok := it.Item().(item.BookAndQuill)
	if !ok {
		return fmt.Errorf("inventory slot %v does not contain a writable book", slot)
	}

	page := edit.Page
	if page >= item.MaxBookPages || page < 0 {
		return fmt.Errorf("page number %v is out of bounds", edit.Page)
	}
	if len(edit.Text) > item.MaxBookPageLength || !utf8.ValidString(edit.Text) {
		return fmt.Errorf("text must be UTF-8 of at most %d bytes", item.MaxBookPageLength)
	}

	switch edit.Action {
	case item.BookReplacePage:
		book = book.SetPage(page, edit.Text)
	case item.BookAddPage:
		if len(book.Pages) >= item.MaxBookPages {
			return fmt.Errorf("unable to add page beyond %d", item.MaxBookPages)
		}
		if page >= len(book.Pages) && page <= len(book.Pages)+2 {
			book = book.SetPage(page, "")
			break
		}
		if _, ok := book.Page(page); !ok {
			return fmt.Errorf("unable to insert page at %v", edit.Page)
		}
		book = book.InsertPage(page, edit.Text)
	case item.BookDeletePage:
		if _, ok := book.Page(page); !ok {
			// The client can be a page or two ahead in the UI of the pages
			// of the book, and still sends the page deleted.
			break
		}
		book = book.DeletePage(page)
	case item.BookSwapPages:
		if edit.Other >= item.MaxBookPages {
			return fmt.Errorf("page number out of bounds")
		}
		_, ok := book.Page(page)
		_, ok2 := book.Page(edit.Other)
		if !ok || !ok2 {
			// The client can try to swap pages that do not exist, as it can
			// be a page or two ahead in the UI of the pages of the book.
			break
		}
		book = book.SwapPages(page, edit.Other)
	case item.BookSign:
		if !item.ValidText(edit.Title, item.MaxBookTitleLength) {
			return fmt.Errorf("title must be UTF-8 of at most %d characters", item.MaxBookTitleLength)
		}
		// The author is the player who signs, whoever the client names.
		_ = p.inv.SetItem(slot, it.WithItem(item.WrittenBook{Title: edit.Title, Author: p.Name(), Pages: book.Pages, Generation: item.OriginalGeneration()}))
		return nil
	}
	_ = p.inv.SetItem(slot, it.WithItem(book))
	return nil
}
