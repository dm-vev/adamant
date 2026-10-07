package javanbt

import (
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

func (p *parser) quoted() (string, error) {
	quote := p.s[p.i]
	p.i++
	start, segment := p.i, p.i
	var out strings.Builder
	escaped := false
	var high rune
	// WTF-8 preserves unpaired Java UTF-16 code units without confusing them
	// with U+FFFD. Adjacent escaped surrogate pairs become ordinary UTF-8.
	flushHigh := func() {
		if high != 0 {
			writeCodeUnit(&out, high)
			high = 0
		}
	}
	writeRaw := func(text string) {
		if text != "" {
			flushHigh()
			out.WriteString(text)
		}
	}
	for p.i < len(p.s) {
		c := p.s[p.i]
		if c == quote {
			end := p.i
			p.i++
			if !escaped {
				return p.s[start:end], nil
			}
			writeRaw(p.s[segment:end])
			flushHigh()
			return out.String(), nil
		}
		if c != '\\' {
			p.i++
			continue
		}
		escaped = true
		writeRaw(p.s[segment:p.i])
		p.i++
		cp, err := p.escape()
		if err != nil {
			return "", err
		}
		if high != 0 && cp >= 0xdc00 && cp <= 0xdfff {
			out.WriteRune(utf16.DecodeRune(high, cp))
			high = 0
		} else {
			flushHigh()
			if cp >= 0xd800 && cp <= 0xdbff {
				high = cp
			} else {
				writeCodeUnit(&out, cp)
			}
		}
		segment = p.i
	}
	return "", p.err("unterminated quoted string")
}

func writeCodeUnit(out *strings.Builder, cp rune) {
	if cp >= 0xd800 && cp <= 0xdfff {
		out.WriteByte(0xe0 | byte(cp>>12))
		out.WriteByte(0x80 | byte(cp>>6&0x3f))
		out.WriteByte(0x80 | byte(cp&0x3f))
	} else {
		out.WriteRune(cp)
	}
}

func (p *parser) escape() (rune, error) {
	p.space() // StringReaderTerms terminals skip Java whitespace.
	if p.i == len(p.s) {
		return 0, p.err("incomplete string escape")
	}
	kind := p.s[p.i]
	p.i++
	switch kind {
	case 'b':
		return '\b', nil
	case 's':
		return ' ', nil
	case 't':
		return '\t', nil
	case 'n':
		return '\n', nil
	case 'f':
		return '\f', nil
	case 'r':
		return '\r', nil
	case '\\', '\'', '"':
		return rune(kind), nil
	case 'x', 'u', 'U':
		length := 2
		if kind == 'u' {
			length = 4
		} else if kind == 'U' {
			length = 8
		}
		if p.i+length > len(p.s) {
			return 0, p.err("incomplete hexadecimal escape")
		}
		text := p.s[p.i : p.i+length]
		for i := range text {
			if !digit(text[i], 16) {
				return 0, p.err("invalid hexadecimal escape")
			}
		}
		n, err := strconv.ParseUint(text, 16, 32)
		if err != nil || n > utf8.MaxRune {
			return 0, p.err("invalid Unicode code point")
		}
		p.i += length
		return rune(n), nil
	case 'N':
		if p.take("{") == 0 {
			return 0, p.err("expected character-name brace")
		}
		start := p.i
		for p.i < len(p.s) && characterNameByte(p.s[p.i]) {
			p.i++
		}
		if p.i == start {
			return 0, p.err("expected Unicode character name")
		}
		name := p.s[start:p.i]
		if p.take("}") == 0 {
			return 0, p.err("expected character-name closing brace")
		}
		if cp, ok := namedCodePoint(name); ok {
			return cp, nil
		}
		return 0, p.err("unknown Unicode character name")
	default:
		return 0, p.err("invalid string escape")
	}
}

func characterNameByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == ' '
}

func namedCodePoint(name string) (rune, bool) {
	name = strings.ToUpper(strings.Trim(name, " "))
	i := sort.Search(len(javaUnicodeNames), func(i int) bool { return javaUnicodeNames[i].name >= name })
	if i < len(javaUnicodeNames) && javaUnicodeNames[i].name == name {
		return javaUnicodeNames[i].codePoint, true
	}
	space := strings.LastIndexByte(name, ' ')
	if space < 0 {
		return 0, false
	}
	hex := name[space+1:]
	if hex == "" || len(hex) > 1 && hex[0] == '0' {
		return 0, false
	}
	for i := range hex {
		if !digit(hex[i], 16) {
			return 0, false
		}
	}
	value, err := strconv.ParseUint(hex, 16, 32)
	if err != nil || value > utf8.MaxRune {
		return 0, false
	}
	cp := rune(value)
	i = sort.Search(len(javaUnicodeNameRanges), func(i int) bool { return javaUnicodeNameRanges[i].max >= cp })
	if i < len(javaUnicodeNameRanges) {
		r := javaUnicodeNameRanges[i]
		return cp, cp >= r.min && name[:space] == r.prefix
	}
	return 0, false
}
