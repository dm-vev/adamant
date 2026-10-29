package session

import "unicode/utf8"

// The bounds of the text a client sends, which the server keeps, passes on
// to other clients or saves: a client names any string the size of a
// packet.
const (
	// maxChatLength bounds a chat message: BDS passes on messages of up to
	// 512 characters and drops longer ones.
	maxChatLength = 512
	// maxBookTitle bounds the title of a book a player signs, as Java does.
	maxBookTitle = 32
	// maxItemName bounds the name a player gives an item in an anvil, as
	// Java does.
	maxItemName = 50
	// maxNBTString bounds a string the server keeps in NBT, as the game
	// reads one: 32767 bytes.
	maxNBTString = 32767
)

// validText reports whether a string a client sent is UTF-8 of at most max
// characters.
func validText(s string, max int) bool {
	return len(s) <= max*utf8.UTFMax && utf8.ValidString(s) && utf8.RuneCountInString(s) <= max
}
