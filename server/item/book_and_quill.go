package item

import "slices"

// BookAndQuill is an item used to write WrittenBook(s).
type BookAndQuill struct {
	// Pages represents the pages within the book.
	Pages []string
}

const (
	maxBookPages     = 50
	maxBookPageBytes = 256
)

// MaxCount always returns 1.
func (BookAndQuill) MaxCount() int {
	return 1
}

// TotalPages returns the total number of pages in the book.
func (b BookAndQuill) TotalPages() int {
	return len(b.Pages)
}

// Page returns a specific page from the book and true when the page exists. It will otherwise return an empty string
// and false.
func (b BookAndQuill) Page(page int) (string, bool) {
	if page < 0 || len(b.Pages) <= page {
		return "", false
	}
	return b.Pages[page], true
}

// DeletePage attempts to delete a page from the book.
func (b BookAndQuill) DeletePage(page int) BookAndQuill {
	if page < 0 || page >= maxBookPages {
		// Ignore invalid input to avoid panics from malformed client data.
		return b
	}
	if _, ok := b.Page(page); !ok {
		return b
	}
	b.Pages = slices.Delete(b.Pages, page, page+1)
	return b
}

// InsertPage attempts to insert a page within the book
func (b BookAndQuill) InsertPage(page int, text string) BookAndQuill {
	if page < 0 || page >= maxBookPages {
		// Ignore invalid input to avoid panics from malformed client data.
		return b
	}
	if len(text) > maxBookPageBytes {
		text = text[:maxBookPageBytes]
	}
	if page > len(b.Pages) {
		return b
	}
	b.Pages = slices.Insert(b.Pages, page, text)
	return b
}

// SetPage writes a page to the book; if the page doesn't exist it will be created.
// Text exceeding maxBookPageBytes is truncated to keep books within protocol limits.
func (b BookAndQuill) SetPage(page int, text string) BookAndQuill {
	if page < 0 || page >= maxBookPages {
		// Ignore invalid input to avoid panics from malformed client data.
		return b
	}
	if len(text) > maxBookPageBytes {
		text = text[:maxBookPageBytes]
	}
	if _, ok := b.Page(page); !ok {
		pages := make([]string, page+1)
		copy(pages, b.Pages)
		b.Pages = pages
	}
	b.Pages[page] = text
	return b
}

// SwapPages swaps two different pages, it will panic if the largest of the two numbers doesn't exist. It will
// return the newly updated pages.
func (b BookAndQuill) SwapPages(pageOne, pageTwo int) BookAndQuill {
	if pageOne < 0 || pageTwo < 0 {
		// Ignore invalid input to avoid panics from malformed client data.
		return b
	}
	if _, ok := b.Page(max(pageOne, pageTwo)); !ok {
		return b
	}
	temp := b.Pages[pageOne]
	b.Pages[pageOne] = b.Pages[pageTwo]
	b.Pages[pageTwo] = temp
	return b
}

// DecodeNBT ...
func (b BookAndQuill) DecodeNBT(data map[string]any) any {
	// Clamp and validate decoded pages to avoid panics and malformed data.
	b.Pages = nil
	pages, _ := data["pages"].([]any)
	if len(pages) > maxBookPages {
		pages = pages[:maxBookPages]
	}
	for _, page := range pages {
		pageData, ok := page.(map[string]any)
		if !ok {
			continue
		}
		text, ok := pageData["text"].(string)
		if !ok {
			continue
		}
		if len(text) > maxBookPageBytes {
			text = text[:maxBookPageBytes]
		}
		b.Pages = append(b.Pages, text)
	}
	return b
}

// EncodeNBT ...
func (b BookAndQuill) EncodeNBT() map[string]any {
	if len(b.Pages) == 0 {
		return nil
	}
	pages := make([]any, 0, min(len(b.Pages), maxBookPages))
	for i, page := range b.Pages {
		if i >= maxBookPages {
			break
		}
		if len(page) > maxBookPageBytes {
			page = page[:maxBookPageBytes]
		}
		pages = append(pages, map[string]any{"text": page})
	}
	return map[string]any{"pages": pages}
}

// EncodeItem ...
func (BookAndQuill) EncodeItem() (name string, meta int16) {
	return "minecraft:writable_book", 0
}
