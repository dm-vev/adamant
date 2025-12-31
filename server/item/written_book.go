package item

// WrittenBook is the item created after a book and quill is signed. It appears the same as a regular book, but
// without the quill, and has an enchanted-looking glint.
type WrittenBook struct {
	// Title is the title of the book.
	Title string
	// Author is the author of the book.
	Author string
	// Generation is the copy tier of the book. 0 = original, 1 = copy of original,
	// 2 = copy of copy.
	Generation WrittenBookGeneration
	// Pages represents the pages within the book.
	Pages []string
}

// MaxCount always returns 16.
func (WrittenBook) MaxCount() int {
	return 16
}

// TotalPages returns the total number of pages in the book.
func (w WrittenBook) TotalPages() int {
	return len(w.Pages)
}

// Page returns a specific page from the book and true when the page exists. It will otherwise return an empty string
// and false.
func (w WrittenBook) Page(page int) (string, bool) {
	if page < 0 || len(w.Pages) <= page {
		return "", false
	}
	return w.Pages[page], true
}

// DecodeNBT ...
func (w WrittenBook) DecodeNBT(data map[string]any) any {
	// Clamp and validate decoded pages to avoid panics and malformed data.
	w.Pages = nil
	if pages, ok := data["pages"].([]any); ok {
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
			w.Pages = append(w.Pages, text)
		}
	}
	w.Title, _ = data["title"].(string)
	w.Author, _ = data["author"].(string)
	if v, ok := data["generation"].(uint8); ok {
		switch v {
		case 0:
			w.Generation = OriginalGeneration()
		case 1:
			w.Generation = CopyGeneration()
		case 2:
			w.Generation = CopyOfCopyGeneration()
		}
	}
	return w
}

// EncodeNBT ...
func (w WrittenBook) EncodeNBT() map[string]any {
	pages := make([]any, 0, min(len(w.Pages), maxBookPages))
	for i, page := range w.Pages {
		if i >= maxBookPages {
			break
		}
		if len(page) > maxBookPageBytes {
			page = page[:maxBookPageBytes]
		}
		pages = append(pages, map[string]any{"text": page})
	}
	return map[string]any{
		"pages":      pages,
		"author":     w.Author,
		"title":      w.Title,
		"generation": w.Generation.Uint8(),
	}
}

// EncodeItem ...
func (WrittenBook) EncodeItem() (name string, meta int16) {
	return "minecraft:written_book", 0
}
