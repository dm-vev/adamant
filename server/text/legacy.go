package text

// legacyCode is what a § code does in Bedrock Edition: it resets, or it
// applies code.
type legacyCode struct {
	code  Code
	reset bool
	ok    bool
}

// legacyCodes holds, by the character after §, the codes of the legacy
// format with their Bedrock Edition meanings. A character with no entry
// makes no code: § and it are text.
var legacyCodes = func() (codes [256]legacyCode) {
	for i, c := range "0123456789abcdef" {
		codes[c] = legacyCode{code: Code{Colour: Black + Colour(i)}, ok: true}
	}
	for c, colour := range map[byte]Colour{
		'g': DarkYellow, 'h': Quartz, 'i': Iron, 'j': Netherite, 'm': Redstone, 'n': Copper,
		'p': Gold, 'q': Emerald, 's': Diamond, 't': Lapis, 'u': Amethyst, 'v': Resin,
	} {
		codes[c] = legacyCode{code: Code{Colour: colour}, ok: true}
	}
	codes['k'] = legacyCode{code: Code{Decoration: Obfuscated}, ok: true}
	codes['l'] = legacyCode{code: Code{Decoration: Bold}, ok: true}
	codes['o'] = legacyCode{code: Code{Decoration: Italic}, ok: true}
	codes['r'] = legacyCode{reset: true, ok: true}
	return codes
}()

// legacyAt returns the code s starts with, if it starts with § and the
// character of a code.
func legacyAt(s string) (legacyCode, bool) {
	if len(s) < 3 || s[0] != 0xC2 || s[1] != 0xA7 {
		return legacyCode{}, false
	}
	c := legacyCodes[s[2]]
	return c, c.ok
}

// ParseLegacy reads s, a legacy string: text with § codes, which it reads
// with their Bedrock Edition meanings, as the package doc describes. It
// keeps every code in its place, even one that changes nothing, so that a
// renderer of § codes writes s back exactly as it is:
//
//   - The text returned has the text of s before its first code as its
//     content, and Reset if s starts with §r.
//   - Each other §r starts a text with Reset, a child of the text
//     returned. If the code of a colour or a decoration follows the §r at
//     once, that text takes it as its style.
//   - Each other code of a colour or a decoration starts a text of that
//     style, a child of the text the code or §r before it started, or of
//     the text returned if there is none.
//   - The text that follows a code, up to the next code, is the content of
//     the text the code started. A text whose colour or decoration no text
//     follows before a §r or the end of s has empty content, so that its
//     code is written.
//
// The empty string gives the zero Text.
func ParseLegacy(s string) Text {
	if s == "" {
		return Text{}
	}
	root := &node{}
	cur := root
	// fresh is set while cur is a text with Reset that has taken no code
	// and no content yet; pending while cur has a code no text followed.
	fresh, pending := false, false
	end := func() {
		if pending {
			cur.kind = literalContent
		}
	}
	for i := 0; i < len(s); {
		c, ok := legacyAt(s[i:])
		if !ok {
			j := i + 1
			for j < len(s) {
				if _, ok := legacyAt(s[j:]); ok {
					break
				}
				j++
			}
			cur.kind, cur.text = literalContent, s[i:j]
			fresh, pending = false, false
			i = j
			continue
		}
		first := i == 0
		i += 3
		switch {
		case c.reset && first:
			root.style.Reset = true
		case c.reset:
			end()
			cur = &node{style: Style{Reset: true}}
			root.children = append(root.children, Text{cur})
			fresh, pending = true, false
		case fresh:
			cur.style = cur.style.with(c.code)
			fresh, pending = false, true
		default:
			n := &node{style: Style{}.with(c.code)}
			cur.children = append(cur.children, Text{n})
			cur = n
			pending = true
		}
	}
	end()
	return Text{root}
}

// with returns s with the colour or the decoration that c applies.
func (s Style) with(c Code) Style {
	if c.Colour != NoColour {
		s.Colour = c.Colour
	} else {
		s.On |= c.Decoration
	}
	return s
}

// legacyDecorations are the decorations a legacy string shows: those of
// Bedrock Edition, which has no underlined or struck through text.
const legacyDecorations = Obfuscated | Bold | Italic

// legacyColours holds the § code of each named colour, legacyDecoration
// that of each decoration Bedrock Edition shows, by the bit of the
// decoration. They are the inverse of legacyCodes.
var legacyColours, legacyDecoration = func() (colours [lastNamed + 1]string, decorations [Italic + 1]string) {
	for c := 0; c < len(legacyCodes); c++ {
		code := legacyCodes[c]
		switch {
		case !code.ok || code.reset:
		case code.code.Colour != NoColour:
			colours[code.code.Colour] = "§" + string(rune(c))
		default:
			decorations[code.code.Decoration] = "§" + string(rune(c))
		}
	}
	return colours, decorations
}()

// legacyReset is the § code of a reset.
const legacyReset = "§r"

// Legacy returns the § code of c: that of a named colour, of the named
// colour nearest to an RGB colour, §k, §l or §o. It returns "" for
// NoColour and for underlined and struck through, which Bedrock Edition
// does not show, and for a Code of more than one decoration.
func (c Code) Legacy() string {
	if c.Colour != NoColour {
		return legacyColours[c.Colour.Nearest()]
	}
	if c.Decoration&^legacyDecorations == 0 && c.Decoration&(c.Decoration-1) == 0 {
		return legacyDecoration[c.Decoration]
	}
	return ""
}

// Legacy returns t as a legacy string: text with § codes in their Bedrock
// Edition meanings, as the world stores text, written as Spans writes a
// stream of codes. A colour of RGB is written as the named colour nearest
// to it; underlined and struck through text is not written, as Bedrock
// Edition has none. Translations are written as their fallbacks, with
// their arguments filled in. For every string s, ParseLegacy(s).Legacy() is
// s; ParseLegacy(t.Legacy()) is Equivalent to t for a text that holds no
// translation, RGB colour, underline or strike-through.
func (t Text) Legacy() string {
	var buf [256]byte
	return string(t.AppendLegacy(buf[:0]))
}

// AppendLegacy appends t as Legacy writes it to dst and returns the result.
func (t Text) AppendLegacy(dst []byte) []byte {
	for s := range t.Spans(SpanOptions{Decorations: legacyDecorations}) {
		if s.Reset {
			dst = append(dst, legacyReset...)
		}
		for _, c := range s.Codes {
			dst = append(dst, c.Legacy()...)
		}
		dst = append(dst, s.Text...)
	}
	return dst
}
