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
// even § codes in it. Where no translation is left to the client, this is
// the legacy writer of server/text, Text.Legacy, which the game also
// writes the text it stores with.
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

// Code returns the § code of c, as Code.Legacy of server/text does.
func Code(c text.Code) string { return c.Legacy() }

// String returns t as a string with § codes, as Text.Legacy of server/text
// writes it. Translations are written as their fallbacks with their
// arguments filled in, as a string that is not translated by the client
// shows them.
func String(t text.Text) string { return t.Legacy() }

// Append appends t as String writes it to dst and returns the result.
func Append(dst []byte, t text.Text) []byte { return t.AppendLegacy(dst) }

// appendSpan appends the reset and the codes of s to dst, and its text.
func appendSpan(dst []byte, s text.Span) []byte {
	if s.Reset {
		dst = append(dst, nettext.Reset...)
	}
	for _, c := range s.Codes {
		dst = append(dst, c.Legacy()...)
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
// A parameter is the argument written as Inline writes it: as String
// writes it, except that a translation in it that has no arguments is
// written as its key after a %, as the game has always sent such a
// parameter for the client to translate.
func Message(t text.Text) (message string, params []string, translated bool) {
	if !holdsTranslation(t) {
		// With no translation to keep, the spans are those String writes,
		// which writes the string a text of ParseLegacy was read from as
		// it is.
		return t.Legacy(), nil, false
	}
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
			params = append(params, Inline(arg))
		}
	}
	return string(dst), params, translated
}

// Inline returns t as a string for a field that a client shows as it is but
// translates keys in: the string with § codes, in which a translation with
// no arguments is written as its key after a %, which the client looks up
// in its language, and one with arguments as its fallback with the
// arguments filled in, as String writes it. Message writes its parameters
// so.
func Inline(t text.Text) string {
	if !holdsTranslation(t) {
		// As in Message.
		return t.Legacy()
	}
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

// holdsTranslation reports whether t is a translation or holds one among
// its children. The arguments of a translation are not looked at: they are
// within one.
func holdsTranslation(t text.Text) bool {
	if _, _, ok := t.Translation(); ok {
		return true
	}
	for c := range t.Children() {
		if holdsTranslation(c) {
			return true
		}
	}
	return false
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
