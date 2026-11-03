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
//   - The text of s up to its first code is the content of the text
//     returned, which has Reset if s starts with §r.
//   - Each code of a colour or a decoration starts a text of that style,
//     a child of the text the code before it started, or of the text
//     returned for the first; the text that follows the code, up to the
//     next code, is its content.
//   - Each §r but a first one starts a text with Reset, a child of the text
//     returned, which takes the code that follows it, if one does, as its
//     style, the text up to the code after as its content, and starts the
//     texts of the codes after as above.
//   - A text whose code is followed by no text before a §r or the end of s
//     has empty content, so that its code is written.
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
