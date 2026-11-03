package player

import (
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

func TestPlayerEditBook(t *testing.T) {
	two := item.BookAndQuill{Pages: []string{"one", "two"}}
	// newTwo returns a book of the pages of two, sharing none of them: the
	// edits of a book and quill write into its pages.
	newTwo := func() item.BookAndQuill { return item.BookAndQuill{Pages: []string{"one", "two"}} }
	cases := []struct {
		name string
		edit item.BookEdit
		want item.BookAndQuill
	}{
		{"replace", item.BookEdit{Action: item.BookReplacePage, Page: 1, Text: "2"}, item.BookAndQuill{Pages: []string{"one", "2"}}},
		{"add after the last", item.BookEdit{Action: item.BookAddPage, Page: 2, Text: "ignored"}, item.BookAndQuill{Pages: []string{"one", "two", ""}}},
		{"insert", item.BookEdit{Action: item.BookAddPage, Page: 0, Text: "zero"}, item.BookAndQuill{Pages: []string{"zero", "one", "two"}}},
		{"delete", item.BookEdit{Action: item.BookDeletePage, Page: 0}, item.BookAndQuill{Pages: []string{"two"}}},
		{"delete a page it lacks", item.BookEdit{Action: item.BookDeletePage, Page: 5}, two},
		{"swap", item.BookEdit{Action: item.BookSwapPages, Page: 0, Other: 1}, item.BookAndQuill{Pages: []string{"two", "one"}}},
		{"swap a page it lacks", item.BookEdit{Action: item.BookSwapPages, Page: 0, Other: 7}, two},
		{"swap a negative page", item.BookEdit{Action: item.BookSwapPages, Page: 0, Other: -1}, two},
		{"no action", item.BookEdit{Page: 0, Text: "x"}, two},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w, _ := clientTestWorld(t)
			withClientPlayer(t, w, nil, func(tx *world.Tx, p *Player) {
				_ = p.Inventory().SetItem(3, item.NewStack(newTwo(), 1).WithCustomName("diary"))
				if err := p.EditBook(3, c.edit); err != nil {
					t.Fatalf("EditBook: %v", err)
				}
				it, _ := p.Inventory().Item(3)
				got, ok := it.Item().(item.BookAndQuill)
				if !ok || strings.Join(got.Pages, "|") != strings.Join(c.want.Pages, "|") {
					t.Fatalf("slot 3 holds %#v, want %#v", it.Item(), c.want)
				}
				if it.CustomName() != "diary" {
					t.Fatalf("the edit lost the name of the stack: %q", it.CustomName())
				}
			})
		})
	}
}

func TestPlayerSignsBookAsItself(t *testing.T) {
	w, _ := clientTestWorld(t)
	withClientPlayer(t, w, nil, func(tx *world.Tx, p *Player) {
		_ = p.Inventory().SetItem(0, item.NewStack(item.BookAndQuill{Pages: []string{"a"}}, 1))
		if err := p.EditBook(0, item.BookEdit{Action: item.BookSign, Title: "Title"}); err != nil {
			t.Fatalf("EditBook: %v", err)
		}
		it, _ := p.Inventory().Item(0)
		book, ok := it.Item().(item.WrittenBook)
		if !ok || book.Title != "Title" || book.Author != p.Name() || len(book.Pages) != 1 || book.Generation != item.OriginalGeneration() {
			t.Fatalf("slot 0 holds %#v, want an original written book by %q", it.Item(), p.Name())
		}
	})
}

func TestPlayerEditBookRefuses(t *testing.T) {
	full := item.BookAndQuill{Pages: make([]string, item.MaxBookPages)}
	cases := []struct {
		name  string
		stack item.Stack
		slot  int
		edit  item.BookEdit
	}{
		{"slot out of range", item.NewStack(item.BookAndQuill{}, 1), 36, item.BookEdit{Action: item.BookReplacePage}},
		{"not a book", item.NewStack(item.Apple{}, 1), 0, item.BookEdit{Action: item.BookReplacePage}},
		{"written book", item.NewStack(item.WrittenBook{}, 1), 0, item.BookEdit{Action: item.BookReplacePage}},
		{"negative page", item.NewStack(item.BookAndQuill{}, 1), 0, item.BookEdit{Action: item.BookReplacePage, Page: -1}},
		{"page 50", item.NewStack(item.BookAndQuill{}, 1), 0, item.BookEdit{Action: item.BookReplacePage, Page: 50}},
		{"long text", item.NewStack(item.BookAndQuill{}, 1), 0, item.BookEdit{Action: item.BookReplacePage, Text: strings.Repeat("a", 257)}},
		{"invalid text", item.NewStack(item.BookAndQuill{}, 1), 0, item.BookEdit{Action: item.BookReplacePage, Text: "a\xff"}},
		{"long text of no action", item.NewStack(item.BookAndQuill{}, 1), 0, item.BookEdit{Text: strings.Repeat("a", 257)}},
		{"add to a full book", item.NewStack(full, 1), 0, item.BookEdit{Action: item.BookAddPage, Page: 0}},
		{"insert far ahead", item.NewStack(item.BookAndQuill{}, 1), 0, item.BookEdit{Action: item.BookAddPage, Page: 3}},
		{"swap with page 50", item.NewStack(item.BookAndQuill{Pages: []string{"a"}}, 1), 0, item.BookEdit{Action: item.BookSwapPages, Other: 50}},
		{"long title", item.NewStack(item.BookAndQuill{}, 1), 0, item.BookEdit{Action: item.BookSign, Title: strings.Repeat("t", item.MaxBookTitleLength+1)}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w, _ := clientTestWorld(t)
			withClientPlayer(t, w, nil, func(tx *world.Tx, p *Player) {
				_ = p.Inventory().SetItem(0, c.stack)
				if err := p.EditBook(c.slot, c.edit); err == nil {
					t.Fatal("EditBook succeeded")
				}
				if it, _ := p.Inventory().Item(0); !it.Equal(c.stack) {
					t.Fatalf("slot 0 holds %#v after a refused edit, want %#v", it, c.stack)
				}
			})
		})
	}
}
