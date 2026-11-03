// Package bedrocktext renders the structured text of the game
// (server/text) as Bedrock Edition shows text: strings with § codes.
//
// It is named bedrocktext, not text, so that an adapter can import it next
// to server/text and net/bedrock/text without renaming them.
//
// A text is written as server/text's Spans writes it, with Bedrock
// Edition's codes: the colours by their codes (an RGB colour as the named
// colour nearest to it), obfuscated, bold and italic as §k, §l and §o, a
// reset as §r. Bedrock Edition has no underlined or struck through text,
// so those decorations are not shown. Literal text is written as it is,
// even § codes in it.
//
// For any markup and arguments, other than arguments that are texts,
// String(text.Colourf(f, a...)) is byte for byte what Colourf of
// net/bedrock/text gives for them, and for any string s,
// String(text.ParseLegacy(s)) is s.
package bedrocktext

import (
	"github.com/df-mc/dragonfly/server/text"
	nettext "github.com/dm-vev/adamant/net/bedrock/text"
)

// decorations are the decorations Bedrock Edition shows.
const decorations = text.Obfuscated | text.Bold | text.Italic

// codes are the § codes of the named colours.
var codes = [...]string{
	text.Black:      nettext.Black,
	text.DarkBlue:   nettext.DarkBlue,
	text.DarkGreen:  nettext.DarkGreen,
	text.DarkAqua:   nettext.DarkAqua,
	text.DarkRed:    nettext.DarkRed,
	text.DarkPurple: nettext.DarkPurple,
	text.Orange:     nettext.Orange,
	text.Grey:       nettext.Grey,
	text.DarkGrey:   nettext.DarkGrey,
	text.Blue:       nettext.Blue,
	text.Green:      nettext.Green,
	text.Aqua:       nettext.Aqua,
	text.Red:        nettext.Red,
	text.Purple:     nettext.Purple,
	text.Yellow:     nettext.Yellow,
	text.White:      nettext.White,
	text.DarkYellow: nettext.DarkYellow,
	text.Quartz:     nettext.Quartz,
	text.Iron:       nettext.Iron,
	text.Netherite:  nettext.Netherite,
	text.Redstone:   nettext.Redstone,
	text.Copper:     nettext.Copper,
	text.Gold:       nettext.Gold,
	text.Emerald:    nettext.Emerald,
	text.Diamond:    nettext.Diamond,
	text.Lapis:      nettext.Lapis,
	text.Amethyst:   nettext.Amethyst,
	text.Resin:      nettext.Resin,
}

// Code returns the § code of c.
func Code(c text.Code) string {
	if c.Colour != text.NoColour {
		if n := c.Colour.Nearest(); n != text.NoColour {
			return codes[n]
		}
		return ""
	}
	switch c.Decoration {
	case text.Obfuscated:
		return nettext.Obfuscated
	case text.Bold:
		return nettext.Bold
	case text.Italic:
		return nettext.Italic
	}
	return ""
}

// String returns t as a string with § codes. Translations are written as
// their fallbacks with their arguments filled in, as a string that is not
// translated by the client shows them.
func String(t text.Text) string {
	var buf [256]byte
	return string(Append(buf[:0], t))
}

// Append appends t as String writes it to dst and returns the result.
func Append(dst []byte, t text.Text) []byte {
	for s := range t.Spans(text.SpanOptions{Decorations: decorations}) {
		dst = appendSpan(dst, s)
	}
	return dst
}

// appendSpan appends the reset and the codes of s to dst, and its text.
func appendSpan(dst []byte, s text.Span) []byte {
	if s.Reset {
		dst = append(dst, nettext.Reset...)
	}
	for _, c := range s.Codes {
		dst = append(dst, Code(c)...)
	}
	return append(dst, s.Text...)
}

// Message returns t as the message of a text the client translates, such
// as a TextPacket that needs translation: the string with § codes, in
// which translations are written as their keys after a %, and the
// arguments of the translations, in order, as its parameters. It reports
// whether t holds a translation; if it does not, the message is what
// String returns and there are no parameters.
//
// A parameter is the argument written as String writes it, except that a
// translation in it that has no arguments is written as its key after a %,
// as the game has always sent such a parameter for the client to
// translate.
func Message(t text.Text) (message string, params []string, translated bool) {
	var buf [256]byte
	dst := buf[:0]
	for s := range t.Spans(text.SpanOptions{Decorations: decorations, Translations: all}) {
		dst = appendSpan(dst, s)
		if s.Translation.IsZero() {
			continue
		}
		translated = true
		key, _, _ := s.Translation.Translation()
		dst = append(dst, '%')
		dst = append(dst, key...)
		for arg := range s.Translation.Args() {
			params = append(params, param(arg))
		}
	}
	return string(dst), params, translated
}

// param returns the argument of a translation as a parameter of a message.
func param(t text.Text) string {
	var buf [128]byte
	dst := buf[:0]
	for s := range t.Spans(text.SpanOptions{Decorations: decorations, Translations: withoutArgs}) {
		dst = appendSpan(dst, s)
		if !s.Translation.IsZero() {
			key, _, _ := s.Translation.Translation()
			dst = append(dst, '%')
			dst = append(dst, key...)
		}
	}
	return string(dst)
}

// all keeps every translation.
func all(text.Text) bool { return true }

// withoutArgs keeps the translations that have no arguments.
func withoutArgs(t text.Text) bool {
	for range t.Args() {
		return false
	}
	return true
}
