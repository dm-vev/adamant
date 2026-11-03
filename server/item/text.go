package item

import "unicode/utf8"

// ValidText reports whether a string a player sent, such as the title of a
// book or the name of an item, is UTF-8 of at most max characters.
func ValidText(s string, max int) bool {
	return len(s) <= max*utf8.UTFMax && utf8.ValidString(s) && utf8.RuneCountInString(s) <= max
}
