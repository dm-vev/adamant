// Package vanilla contains small compatibility helpers for Java 26.3 parsing.
package vanilla

import (
	"fmt"
	"unicode"
)

// ParseSignedInteger matches Java's Integer.parseInt/Long.parseLong digit and
// signed-range behavior for a radix from 2 through 36. Like Java's char-based
// parsers, it accepts BMP Character.digit characters but not supplementary
// code points.
func ParseSignedInteger(text string, radix, bitSize int) (int64, error) {
	if radix < 2 || radix > 36 || bitSize < 1 || bitSize > 64 {
		return 0, fmt.Errorf("invalid radix or bit size")
	}
	if text == "" {
		return 0, fmt.Errorf("empty integer")
	}
	negative := text[0] == '-'
	if negative || text[0] == '+' {
		text = text[1:]
	}
	if text == "" {
		return 0, fmt.Errorf("expected integer digits")
	}
	limit := -int64((uint64(1) << (bitSize - 1)) - 1)
	if negative {
		limit--
	}
	multiplyLimit := limit / int64(radix)
	var value int64
	for _, r := range text {
		digit, ok := digit(r)
		if !ok || digit >= radix {
			return 0, fmt.Errorf("invalid integer digit")
		}
		if value < multiplyLimit {
			return 0, fmt.Errorf("integer out of range")
		}
		value *= int64(radix)
		if value < limit+int64(digit) {
			return 0, fmt.Errorf("integer out of range")
		}
		value -= int64(digit)
	}
	if negative {
		return value, nil
	}
	return -value, nil
}

func digit(r rune) (int, bool) {
	if r > 0xffff {
		return 0, false
	}
	if r >= '0' && r <= '9' {
		return int(r - '0'), true
	}
	if r >= 'a' && r <= 'z' {
		return int(r-'a') + 10, true
	}
	if r >= 'A' && r <= 'Z' {
		return int(r-'A') + 10, true
	}
	if r >= '\uff41' && r <= '\uff5a' {
		return int(r-'\uff41') + 10, true
	}
	if r >= '\uff21' && r <= '\uff3a' {
		return int(r-'\uff21') + 10, true
	}
	// R16 suffices: Java's parseInt/parseLong visit UTF-16 code units.
	for _, span := range unicode.Digit.R16 {
		if r < rune(span.Lo) {
			break
		}
		if uint16(r) >= span.Lo && uint16(r) <= span.Hi && (uint16(r)-span.Lo)%span.Stride == 0 {
			return int(((uint16(r) - span.Lo) / span.Stride) % 10), true
		}
	}
	return 0, false
}
