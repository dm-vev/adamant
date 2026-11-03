// Package javatext renders the structured text of the game (server/text)
// as Java Edition text components in their JSON form, as Java Edition 26.3
// reads them. It is named javatext, not text, so that an adapter can import
// it next to server/text without renaming either.
//
// Its purpose for now is to show that the model of server/text is not
// bound to Bedrock Edition; it is small, and parts of it have not been
// checked against the game yet.
//
// A text is written as a component: literal text as "text", a translation
// as "translate" with "fallback" if it has one and its arguments as
// "with", a text with no content as an empty "text", and its children as
// "extra". A literal text with no style and no children is written as a
// JSON string, which is short for such a component. The style is written
// as "color", the colours both editions have by their names, Orange as
// "gold", Grey as "gray", Purple as "light_purple" and so on, the colours
// of materials and RGB colours as "#RRGGBB", and the decorations as
// "bold", "italic", "underlined", "strikethrough" and "obfuscated": true
// for those turned on, false for those turned off.
//
// Components inherit the style of their parents as texts do. A text with
// Reset inherits nothing, which components cannot say, so the renderer
// overrides what the text would inherit from the texts it is within: each
// decoration on there is written as false, and an inherited colour as
// "white". The Reset of the outermost text is not written: the renderer
// does not know the style of where the text is shown.
//
// Not yet checked against the game:
//   - The format itself as 26.3 reads it, beyond the keys above; on the
//     network, components are sent as NBT, not JSON, which an adapter
//     still has to write.
//   - "white" as the colour of a text with Reset, where the text is shown
//     in another colour, as on signs and in lore, and the Reset of the
//     outermost text, which Bedrock Edition uses to show names of items
//     without the italic of renamed items.
//   - § codes in literal text, which the renderer writes as they are and
//     the client may read with Java Edition's meanings (§m and §n struck
//     through and underlined).
//   - Translations: the keys of the game are those of Bedrock Edition,
//     which Java Edition names differently in many cases.
//   - Texts nested deeper than the client allows.
package javatext

import (
	"unicode/utf8"

	"github.com/df-mc/dragonfly/server/text"
)

// names are the names of the colours both editions have.
var names = map[text.Colour]string{
	text.Black:      "black",
	text.DarkBlue:   "dark_blue",
	text.DarkGreen:  "dark_green",
	text.DarkAqua:   "dark_aqua",
	text.DarkRed:    "dark_red",
	text.DarkPurple: "dark_purple",
	text.Orange:     "gold",
	text.Grey:       "gray",
	text.DarkGrey:   "dark_gray",
	text.Blue:       "blue",
	text.Green:      "green",
	text.Aqua:       "aqua",
	text.Red:        "red",
	text.Purple:     "light_purple",
	text.Yellow:     "yellow",
	text.White:      "white",
}

// decorations are the keys of the decorations, in their order.
var decorations = [...]struct {
	d   text.Decoration
	key string
}{
	{text.Obfuscated, "obfuscated"},
	{text.Bold, "bold"},
	{text.Strikethrough, "strikethrough"},
	{text.Underlined, "underlined"},
	{text.Italic, "italic"},
}

// JSON returns t as a text component in JSON. The zero Text is the empty
// string, "".
func JSON(t text.Text) string {
	return string(AppendJSON(nil, t))
}

// AppendJSON appends t as JSON writes it to dst and returns the result.
func AppendJSON(dst []byte, t text.Text) []byte {
	return appendComponent(dst, t, text.Style{}, true)
}

// appendComponent appends the component of t, which inherits the style in
// effect inherited, to dst. outermost is set for the outermost text.
func appendComponent(dst []byte, t text.Text, inherited text.Style, outermost bool) []byte {
	if t.IsZero() {
		return append(dst, `""`...)
	}
	s := t.Style()
	if s.Reset && outermost {
		s.Reset = false
	}
	lit, isLiteral := t.Literal()
	hasChildren := false
	for range t.Children() {
		hasChildren = true
		break
	}
	if isLiteral && !hasChildren && s == (text.Style{}) {
		return appendString(dst, lit)
	}

	dst = append(dst, '{')
	if key, fallback, ok := t.Translation(); ok {
		dst = appendKey(dst, "translate", true)
		dst = appendString(dst, key)
		if fallback != "" {
			dst = appendKey(dst, "fallback", false)
			dst = appendString(dst, fallback)
		}
	} else {
		dst = appendKey(dst, "text", true)
		dst = appendString(dst, lit)
	}

	colour, on, off := s.Colour, s.On, s.Off
	if s.Reset {
		// Override what the text would inherit.
		if colour == text.NoColour && inherited.Colour != text.NoColour {
			colour = text.White
		}
		off = inherited.On &^ on
	}
	if colour != text.NoColour {
		dst = appendKey(dst, "color", false)
		dst = appendString(dst, colourName(colour))
	}
	for _, d := range decorations {
		switch {
		case on&d.d != 0:
			dst = appendKey(dst, d.key, false)
			dst = append(dst, "true"...)
		case off&d.d != 0:
			dst = appendKey(dst, d.key, false)
			dst = append(dst, "false"...)
		}
	}

	effect := s.Inherit(inherited)
	if _, _, ok := t.Translation(); ok {
		first := true
		for arg := range t.Args() {
			if first {
				dst = appendKey(dst, "with", false)
				dst = append(dst, '[')
			} else {
				dst = append(dst, ',')
			}
			first = false
			dst = appendComponent(dst, arg, effect, false)
		}
		if !first {
			dst = append(dst, ']')
		}
	}
	if hasChildren {
		dst = appendKey(dst, "extra", false)
		dst = append(dst, '[')
		first := true
		for c := range t.Children() {
			if !first {
				dst = append(dst, ',')
			}
			first = false
			dst = appendComponent(dst, c, effect, false)
		}
		dst = append(dst, ']')
	}
	return append(dst, '}')
}

// colourName returns the name of c in a component: its name, or its RGB
// value as #RRGGBB.
func colourName(c text.Colour) string {
	if name, ok := names[c]; ok {
		return name
	}
	r, g, b := c.RGB()
	const hex = "0123456789ABCDEF"
	return string([]byte{'#', hex[r>>4], hex[r&15], hex[g>>4], hex[g&15], hex[b>>4], hex[b&15]})
}

// appendKey appends the key of a member of an object, after a comma unless
// it is the first.
func appendKey(dst []byte, key string, first bool) []byte {
	if !first {
		dst = append(dst, ',')
	}
	dst = appendString(dst, key)
	return append(dst, ':')
}

// appendString appends s as a JSON string. Bytes that are not UTF-8 are
// written as U+FFFD, as encoding/json writes them.
func appendString(dst []byte, s string) []byte {
	dst = append(dst, '"')
	for i := 0; i < len(s); {
		c := s[i]
		if c < utf8.RuneSelf {
			switch {
			case c == '"' || c == '\\':
				dst = append(dst, '\\', c)
			case c == '\n':
				dst = append(dst, `\n`...)
			case c == '\r':
				dst = append(dst, `\r`...)
			case c == '\t':
				dst = append(dst, `\t`...)
			case c < 0x20:
				dst = append(dst, `\u00`...)
				dst = append(dst, "0123456789abcdef"[c>>4], "0123456789abcdef"[c&15])
			default:
				dst = append(dst, c)
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			dst = append(dst, "\uFFFD"...)
		} else {
			dst = append(dst, s[i:i+size]...)
		}
		i += size
	}
	return append(dst, '"')
}
