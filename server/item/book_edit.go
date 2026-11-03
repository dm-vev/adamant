package item

// BookAction is what a BookEdit does to a book and quill. The zero BookAction
// does nothing: the book is written back unchanged.
type BookAction int

const (
	// BookReplacePage replaces the text of the page Page with Text, adding
	// pages up to it if the book has fewer.
	BookReplacePage BookAction = iota + 1
	// BookAddPage adds an empty page Page right after the last pages, or
	// inserts a page with Text at Page.
	BookAddPage
	// BookDeletePage deletes the page Page, if the book has it.
	BookDeletePage
	// BookSwapPages swaps the pages Page and Other, if the book has both.
	BookSwapPages
	// BookSign signs the book, which becomes a WrittenBook titled Title.
	BookSign
)

// BookEdit is one edit a player makes to a book and quill it holds, as its
// client sends it while the player writes in the book.
type BookEdit struct {
	// Action is what the edit does.
	Action BookAction
	// Page is the page the edit applies to and Other the page swapped with
	// it by BookSwapPages.
	Page, Other int
	// Text is the text of the page set or inserted, and Title the title of
	// the book signed.
	Text, Title string
}

const (
	// MaxBookPages is the number of pages a book and quill holds at most.
	MaxBookPages = 50
	// MaxBookPageLength bounds the text of a page of a book and quill, in
	// bytes.
	MaxBookPageLength = 256
	// MaxBookTitleLength bounds the title of a book a player signs, in
	// characters, as Java does.
	MaxBookTitleLength = 32
)
