package java26

// IsWhitespace matches Character.isWhitespace in the pinned JDK 25. Unlike
// Unicode White_Space, it excludes non-breaking spaces and includes U+001C..001F.
func IsWhitespace(r rune) bool {
	return r >= '\t' && r <= '\r' || r >= 0x1c && r <= 0x20 || r == 0x1680 ||
		r >= 0x2000 && r <= 0x2006 || r >= 0x2008 && r <= 0x200a ||
		r == 0x2028 || r == 0x2029 || r == 0x205f || r == 0x3000
}
