package session

// The bounds of the text a client sends, which the server keeps, passes on
// to other clients or saves: a client names any string the size of a
// packet.
const (
	// maxChatLength bounds a chat message: BDS passes on messages of up to
	// 512 characters and drops longer ones.
	maxChatLength = 512
	// maxNBTString bounds a string the server keeps in NBT, as the game
	// reads one: 32767 bytes.
	maxNBTString = 32767
)
