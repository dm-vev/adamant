package item

import (
	"slices"
	"testing"
)

// sharedBook returns a book and quill whose pages have spare capacity, as
// pages that were appended to or inserted into do, so that an insertion can
// write into the array a copy of the book shares.
func sharedBook() BookAndQuill {
	pages := make([]string, 3, 8)
	copy(pages, []string{"one", "two", "three"})
	return BookAndQuill{Pages: pages}
}

func TestBookAndQuillEditsDoNotTouchCopies(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(BookAndQuill) BookAndQuill
		want []string
	}{
		{"SetPage replaces", func(b BookAndQuill) BookAndQuill { return b.SetPage(1, "edited") }, []string{"one", "edited", "three"}},
		{"SetPage extends", func(b BookAndQuill) BookAndQuill { return b.SetPage(4, "far") }, []string{"one", "two", "three", "", "far"}},
		{"InsertPage", func(b BookAndQuill) BookAndQuill { return b.InsertPage(1, "new") }, []string{"one", "new", "two", "three"}},
		{"DeletePage", func(b BookAndQuill) BookAndQuill { return b.DeletePage(0) }, []string{"two", "three"}},
		{"SwapPages", func(b BookAndQuill) BookAndQuill { return b.SwapPages(0, 2) }, []string{"three", "two", "one"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := sharedBook()
			cp := original
			stack := NewStack(original, 1)

			edited := tc.edit(cp)
			if !slices.Equal(edited.Pages, tc.want) {
				t.Fatalf("edited pages = %q, want %q", edited.Pages, tc.want)
			}
			if want := []string{"one", "two", "three"}; !slices.Equal(original.Pages, want) {
				t.Fatalf("original pages = %q after editing a copy, want %q", original.Pages, want)
			}
			if got := stack.Item().(BookAndQuill).Pages; !slices.Equal(got, []string{"one", "two", "three"}) {
				t.Fatalf("stack pages = %q after editing a copy, want unchanged", got)
			}
			// The spare capacity must not have been written either.
			if tail := original.Pages[:cap(original.Pages)][3:5]; tail[0] != "" || tail[1] != "" {
				t.Fatalf("spare capacity of the original written to: %q", tail)
			}
		})
	}
}
