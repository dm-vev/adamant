// Package text formats the text of Minecraft: the colour and format codes
// of the game (§ and a character), tags that stand for them, and their
// ANSI counterparts for a terminal.
package text

import (
	"fmt"
	"html"
	"strings"
)

// Codes of the colours and formats of the game.
const (
	Black      = "§0"
	DarkBlue   = "§1"
	DarkGreen  = "§2"
	DarkAqua   = "§3"
	DarkRed    = "§4"
	DarkPurple = "§5"
	Orange     = "§6"
	Grey       = "§7"
	DarkGrey   = "§8"
	Blue       = "§9"
	Green      = "§a"
	Aqua       = "§b"
	Red        = "§c"
	Purple     = "§d"
	Yellow     = "§e"
	White      = "§f"
	DarkYellow = "§g"
	Quartz     = "§h"
	Iron       = "§i"
	Netherite  = "§j"
	Obfuscated = "§k"
	Bold       = "§l"
	Redstone   = "§m"
	Copper     = "§n"
	Italic     = "§o"
	Gold       = "§p"
	Emerald    = "§q"
	Reset      = "§r"
	Diamond    = "§s"
	Lapis      = "§t"
	Amethyst   = "§u"
	Resin      = "§v"
)

// Clean removes all formatting codes of the game from s.
func Clean(s string) string {
	if !strings.Contains(s, "§") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if strings.HasPrefix(s[i:], "§") && i+2 < len(s) && isCode(s[i+2]) {
			i += 2
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// isCode reports whether c is the character of a formatting code.
func isCode(c byte) bool { return c >= '0' && c <= '9' || c >= 'a' && c <= 'v' }

// ANSI converts the formatting codes of the game in the values passed to
// ANSI codes, so that a terminal shows them.
func ANSI(a ...any) string {
	str := make([]string, len(a))
	for i, v := range a {
		str[i] = minecraftReplacer.Replace(fmt.Sprint(v))
	}
	return strings.Join(str, " ")
}

// Colourf formats the format string with the values passed, then turns the
// tags in it into formatting codes. The tags are the colours black,
// dark-blue, dark-green, dark-aqua, dark-red, dark-purple, orange, grey,
// dark-grey, blue, green, aqua, red, purple, yellow, white, dark-yellow,
// quartz, iron, netherite, redstone, copper, gold, emerald, diamond, lapis,
// amethyst and resin, and obfuscated, bold (b) and italic (i), which may be
// nested: `<red>Hello <bold>World</bold>!</red>`. The rest is read as HTML
// is: other tags are kept as text, comments and self-closing tags dropped,
// character references unescaped.
func Colourf(format string, a ...any) string {
	e := encoder{}
	tokenize(fmt.Sprintf(format, a...), e.process)
	return e.b.String()
}

// encoder turns the tokens of a text into formatting codes. A text of no
// token at all comes out empty, others start with a reset.
type encoder struct {
	b       strings.Builder
	stack   []string
	started bool
}

func (e *encoder) process(t token) {
	if !e.started {
		e.b.WriteString(Reset)
		e.started = true
	}
	switch t.kind {
	case textToken:
		e.write(t.data)
	case startTagToken:
		if format, ok := strMap[t.data]; ok {
			e.stack = append(e.stack, format)
			return
		}
		e.write("<" + t.data + ">")
	case endTagToken:
		if format, ok := strMap[t.data]; ok && removeFirst(&e.stack, format) {
			return
		}
		e.write("</" + t.data + ">")
	}
}

// write writes text in the formats of the stack.
func (e *encoder) write(s string) {
	for _, f := range e.stack {
		e.b.WriteString(f)
	}
	e.b.WriteString(s)
	if len(e.stack) != 0 {
		e.b.WriteString(Reset)
	}
}

// Kinds of the tokens of a text: otherToken stands for comments, doctypes
// and self-closing tags.
const (
	textToken = iota
	startTagToken
	endTagToken
	otherToken
)

// token is a token of HTML: text, unescaped, or the name of a tag in lower
// case.
type token struct {
	kind int
	data string
}

// rawTags are the elements whose content is text up to their end tag;
// those of rcdataTags have their character references unescaped.
var (
	rawTags    = map[string]bool{"iframe": true, "noembed": true, "noframes": true, "noscript": true, "plaintext": true, "script": true, "style": true, "textarea": true, "title": true, "xmp": true}
	rcdataTags = map[string]bool{"textarea": true, "title": true}
)

// tokenize reads s as the tokenizer of HTML does and hands its tokens to
// f. A tag cut off by the end is dropped.
func tokenize(s string, f func(token)) {
	var text strings.Builder
	flush := func() {
		if text.Len() > 0 {
			f(token{textToken, newlines(html.UnescapeString(text.String()))})
			text.Reset()
		}
	}
	for len(s) > 0 {
		i := strings.IndexByte(s, '<')
		if i < 0 {
			text.WriteString(s)
			break
		}
		text.WriteString(s[:i])
		s = s[i:]
		if len(s) < 2 {
			// A bracket at the end is text.
			text.WriteString(s)
			break
		}
		c := s[1]
		switch {
		case isLetter(c):
			name, selfClosing, n := readTag(s[1:])
			if n < 0 {
				// The end cut the tag off.
				flush()
				return
			}
			s = s[1+n:]
			if selfClosing {
				flush()
				f(token{kind: otherToken})
				continue
			}
			flush()
			f(token{startTagToken, name})
			if rawTags[name] {
				var (
					raw    string
					closed bool
				)
				raw, s, closed = rawText(s, name)
				if raw != "" {
					if rcdataTags[name] {
						raw = html.UnescapeString(raw)
					}
					f(token{textToken, strings.ReplaceAll(newlines(raw), "\x00", "\uFFFD")})
				}
				if closed {
					f(token{endTagToken, name})
				}
			}
		case c == '/':
			if len(s) < 3 {
				// "</" at the end is text of its own.
				flush()
				f(token{textToken, s})
				return
			}
			flush()
			switch {
			case isLetter(s[2]):
				name, _, n := readTag(s[2:])
				if n < 0 {
					return
				}
				s = s[2+n:]
				f(token{endTagToken, name})
			case s[2] == '>':
				// Dropped, as a comment would be.
				s = s[3:]
				f(token{kind: otherToken})
			default:
				// A bogus comment, up to the next '>' or the end.
				s, _ = skipPast(s[2:], ">")
				f(token{kind: otherToken})
			}
		case c == '!':
			flush()
			// A comment, a doctype or a bogus comment, which the end of s
			// ends too.
			if strings.HasPrefix(s, "<!--") {
				s, _ = skipComment(s[4:])
			} else {
				s, _ = skipPast(s[2:], ">")
			}
			f(token{kind: otherToken})
		case c == '?':
			flush()
			s, _ = skipPast(s[2:], ">")
			f(token{kind: otherToken})
		default:
			text.WriteByte('<')
			s = s[1:]
		}
	}
	flush()
}

// readTag reads the name and attributes of a tag after its '<' or '</':
// its name in lower case, whether it closes itself, and the bytes up to and
// past its '>', -1 if the end of s cut it off. Attributes are read as HTML
// reads them: a quote starts a value only after '='.
func readTag(s string) (name string, selfClosing bool, n int) {
	i := 0
	for i < len(s) && !isSpace(s[i]) && s[i] != '/' && s[i] != '>' {
		i++
	}
	name = asciiLower(s[:i])
	for {
		for i < len(s) && isSpace(s[i]) {
			i++
		}
		if i >= len(s) {
			return "", false, -1
		}
		switch s[i] {
		case '>':
			return name, false, i + 1
		case '/':
			if i+1 < len(s) && s[i+1] == '>' {
				return name, true, i + 2
			}
			i++
			continue
		}
		// The name of an attribute, a first '=' included.
		i++
		for i < len(s) && !isSpace(s[i]) && s[i] != '/' && s[i] != '>' && s[i] != '=' {
			i++
		}
		for i < len(s) && isSpace(s[i]) {
			i++
		}
		if i >= len(s) || s[i] != '=' {
			continue
		}
		i++
		for i < len(s) && isSpace(s[i]) {
			i++
		}
		if i < len(s) && (s[i] == '"' || s[i] == '\'') {
			end := strings.IndexByte(s[i+1:], s[i])
			if end < 0 {
				return "", false, -1
			}
			i += end + 2
			continue
		}
		for i < len(s) && !isSpace(s[i]) && s[i] != '>' {
			i++
		}
	}
}

// rawText returns the text of a raw element up to its end tag, what
// follows the end tag, and whether there was one.
func rawText(s, name string) (string, string, bool) {
	if name == "plaintext" {
		return s, "", false
	}
	for off := 0; ; {
		i := indexEndTag(s[off:], name)
		if i < 0 {
			return s, "", false
		}
		i += off
		j := i + 2 + len(name)
		if j == len(s) || isSpace(s[j]) || s[j] == '/' || s[j] == '>' {
			_, _, n := readTag(s[i+2:])
			if n < 0 {
				return s[:i], "", false
			}
			return s[:i], s[i+2+n:], true
		}
		off = j
	}
}

// newlines turns the line breaks of s into "\n", as HTML reads "\r\n" and
// "\r".
func newlines(s string) string {
	if !strings.Contains(s, "\r") {
		return s
	}
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
}

// indexEndTag returns where "</" and name start in s, the letters of the
// name in any case, -1 if they do not. It compares bytes as ASCII, so
// that no index moves, as one into a string lowered as Unicode would.
func indexEndTag(s, name string) int {
	for i := 0; i+2+len(name) <= len(s); i++ {
		if s[i] != '<' || s[i+1] != '/' {
			continue
		}
		match := true
		for j := 0; j < len(name); j++ {
			c := s[i+2+j]
			if c >= 'A' && c <= 'Z' {
				c += 'a' - 'A'
			}
			if c != name[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// skipPast returns what follows the first sep of s, and false if there is
// none.
func skipPast(s, sep string) (string, bool) {
	_, after, ok := strings.Cut(s, sep)
	return after, ok
}

// skipComment returns what follows the comment whose body s starts with,
// and false if the end cut it off.
func skipComment(s string) (string, bool) {
	// "<!-->" and "<!--->" end at once.
	if strings.HasPrefix(s, ">") {
		return s[1:], true
	}
	if strings.HasPrefix(s, "->") {
		return s[2:], true
	}
	for i := 0; i < len(s); i++ {
		switch {
		case strings.HasPrefix(s[i:], "-->"):
			return s[i+3:], true
		case strings.HasPrefix(s[i:], "--!>"):
			return s[i+4:], true
		}
	}
	return "", false
}

// asciiLower lowers the ASCII letters of s alone, as HTML lowers the names
// of tags.
func asciiLower(s string) string {
	for i := 0; i < len(s); i++ {
		if c := s[i]; c >= 'A' && c <= 'Z' {
			b := []byte(s)
			for j := i; j < len(b); j++ {
				if b[j] >= 'A' && b[j] <= 'Z' {
					b[j] += 'a' - 'A'
				}
			}
			return string(b)
		}
	}
	return s
}

func isLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
func isSpace(c byte) bool  { return c == ' ' || c == '\t' || c == '\n' || c == '\f' || c == '\r' }

// removeFirst removes the first f of the stack and reports whether it was
// there.
func removeFirst(stack *[]string, f string) bool {
	for i, s := range *stack {
		if s == f {
			*stack = append((*stack)[:i], (*stack)[i+1:]...)
			return true
		}
	}
	return false
}

const (
	ansiBlack      = "\x1b[38;5;16m"
	ansiDarkBlue   = "\x1b[38;5;19m"
	ansiDarkGreen  = "\x1b[38;5;34m"
	ansiDarkAqua   = "\x1b[38;5;37m"
	ansiDarkRed    = "\x1b[38;5;124m"
	ansiDarkPurple = "\x1b[38;5;127m"
	ansiOrange     = "\x1b[38;5;214m"
	ansiGrey       = "\x1b[38;5;145m"
	ansiDarkGrey   = "\x1b[38;5;59m"
	ansiBlue       = "\x1b[38;5;63m"
	ansiGreen      = "\x1b[38;5;83m"
	ansiAqua       = "\x1b[38;5;87m"
	ansiRed        = "\x1b[38;5;203m"
	ansiPurple     = "\x1b[38;5;207m"
	ansiYellow     = "\x1b[38;5;227m"
	ansiWhite      = "\x1b[38;5;231m"
	ansiDarkYellow = "\x1b[38;5;226m"
	ansiQuartz     = "\x1b[38;5;224m"
	ansiIron       = "\x1b[38;5;251m"
	ansiNetherite  = "\x1b[38;5;234m"
	ansiRedstone   = "\x1b[38;5;1m"
	ansiCopper     = "\x1b[38;5;216m"
	ansiGold       = "\x1b[38;5;220m"
	ansiEmerald    = "\x1b[38;5;71m"
	ansiDiamond    = "\x1b[38;5;122m"
	ansiLapis      = "\x1b[38;5;4m"
	ansiAmethyst   = "\x1b[38;5;171m"
	ansiResin      = "\x1b[38;5;172m"

	ansiObfuscated = ""
	ansiBold       = "\x1b[1m"
	ansiItalic     = "\x1b[3m"
	ansiReset      = "\x1b[m"
)

var m = map[string]string{
	Black:      ansiBlack,
	DarkBlue:   ansiDarkBlue,
	DarkGreen:  ansiDarkGreen,
	DarkAqua:   ansiDarkAqua,
	DarkRed:    ansiDarkRed,
	DarkPurple: ansiDarkPurple,
	Orange:     ansiOrange,
	Grey:       ansiGrey,
	DarkGrey:   ansiDarkGrey,
	Blue:       ansiBlue,
	Green:      ansiGreen,
	Aqua:       ansiAqua,
	Red:        ansiRed,
	Purple:     ansiPurple,
	Yellow:     ansiYellow,
	White:      ansiWhite,
	DarkYellow: ansiDarkYellow,
	Quartz:     ansiQuartz,
	Iron:       ansiIron,
	Netherite:  ansiNetherite,
	Redstone:   ansiRedstone,
	Copper:     ansiCopper,
	Gold:       ansiGold,
	Emerald:    ansiEmerald,
	Diamond:    ansiDiamond,
	Lapis:      ansiLapis,
	Amethyst:   ansiAmethyst,
	Resin:      ansiResin,

	Obfuscated: ansiObfuscated,
	Bold:       ansiBold,
	Reset:      ansiReset,
	Italic:     ansiItalic,
}

var strMap = map[string]string{
	"black":       Black,
	"dark-blue":   DarkBlue,
	"dark-green":  DarkGreen,
	"dark-aqua":   DarkAqua,
	"dark-red":    DarkRed,
	"dark-purple": DarkPurple,
	"orange":      Orange,
	"grey":        Grey,
	"dark-grey":   DarkGrey,
	"blue":        Blue,
	"green":       Green,
	"aqua":        Aqua,
	"red":         Red,
	"purple":      Purple,
	"yellow":      Yellow,
	"white":       White,
	"dark-yellow": DarkYellow,
	"quartz":      Quartz,
	"iron":        Iron,
	"netherite":   Netherite,
	"obfuscated":  Obfuscated,
	"bold":        Bold,
	"b":           Bold,
	"redstone":    Redstone,
	"copper":      Copper,
	"gold":        Gold,
	"emerald":     Emerald,
	"italic":      Italic,
	"i":           Italic,
	"diamond":     Diamond,
	"lapis":       Lapis,
	"amethyst":    Amethyst,
	"resin":       Resin,
}

// minecraftReplacer and ansiReplacer are used to translate ANSI formatting codes to Minecraft formatting
// codes and vice versa.
// minecraftReplacer turns the formatting codes of the game into ANSI codes.
var minecraftReplacer = func() *strings.Replacer {
	var pairs []string
	for code, ansi := range m {
		pairs = append(pairs, code, ansi)
	}
	return strings.NewReplacer(pairs...)
}()
