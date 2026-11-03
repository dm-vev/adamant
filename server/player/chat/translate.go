package chat

import (
	"fmt"
	"strings"

	"github.com/df-mc/dragonfly/server/text"
	"golang.org/x/text/language"
)

// https://github.com/Mojang/bedrock-samples/blob/main/resource_pack/texts/en_GB.lang

var MessageJoin = Translate(str("%multiplayer.player.joined"), 1, `%s joined the game`).Enc("<yellow>%v</yellow>")
var MessageQuit = Translate(str("%multiplayer.player.left"), 1, `%s left the game`).Enc("<yellow>%v</yellow>")
var MessageServerDisconnect = Translate(str("%disconnect.disconnected"), 0, `Disconnected by Server`).Enc("<yellow>%v</yellow>")

var MessageBedTooFar = Translate(str("%tile.bed.tooFar"), 0, `Bed is too far away`).Enc("<grey>%v</grey>")
var MessageBedObstructed = Translate(str("%tile.bed.obstructed"), 0, `Bed is obstructed`).Enc("<grey>%v</grey>")
var MessageRespawnPointSet = Translate(str("%tile.bed.respawnSet"), 0, `Respawn point set`).Enc("<grey>%v</grey>")
var MessageNoSleep = Translate(str("%tile.bed.noSleep"), 0, `You can only sleep at night and during thunderstorms`).Enc("<grey>%v</grey>")
var MessageNoSleepMonsters = Translate(str("%tile.bed.notSafe"), 0, `You may not rest now; there are monsters nearby`).Enc("<grey>%v</grey>")
var MessageBedIsOccupied = Translate(str("%tile.bed.occupied"), 0, `This bed is occupied`).Enc("<grey>%v</grey>")
var MessageSleeping = Translate(str("%chat.type.sleeping"), 2, `%s is sleeping in a bed. To skip to dawn, %s more users need to sleep in beds at the same time.`)
var MessageRespawnAnchorNotValid = Translate(str("%tile.respawn_anchor.notValid"), 0, `Your respawn anchor was out of charges, missing or obstructed`).Enc("<grey>%v</grey>")
var MessageBedNotValid = Translate(str("%tile.bed.notValid"), 0, `Your home bed was missing or obstructed`)
var MessageAdminCommand = Translate(str("%chat.type.admin"), 2, `[%s: %s]`).Enc("<grey><i>%v</i></grey>")

type str string

// Resolve returns the translation identifier as a string.
func (s str) Resolve(language.Tag) string { return string(s) }

// TranslationString is a value that can resolve a translated version of itself
// for a language.Tag passed.
type TranslationString interface {
	// Resolve finds a suitable translated version for a translation string for
	// a specific language.Tag.
	Resolve(l language.Tag) string
}

// Texter is a value that is text in the language of whoever is shown it,
// such as a translation filled in by Translation.F.
type Texter interface {
	// Text returns the value as text for a reader of the language l.
	Text(l language.Tag) text.Text
}

// Translate returns a Translation for a TranslationString. The required number
// of parameters specifies how many arguments may be passed to Translation.F.
//
// str resolves, for a language, to the key of the translation after a %, as
// in "%tile.bed.tooFar"; a string without the % is no key but text of the
// server's own, shown as it is. fallback is the text shown where the key is
// not looked up, such as on the console: a format as text.Translate takes
// it, %s for the next argument, %n$s for the nth and %% for a percent sign.
// In the fallback of a translation with parameters, %v is read as %s, as
// fallbacks were written before; that of a translation without parameters
// is shown exactly as it is.
func Translate(str TranslationString, params int, fallback string) Translation {
	if params == 0 {
		fallback = strings.ReplaceAll(fallback, "%", "%%")
	} else {
		fallback = placeholders(fallback)
	}
	return Translation{str: str, params: params, fallback: fallback, format: "%v"}
}

// placeholders returns the fallback f with every %v that is not part of %%
// written as %s.
func placeholders(f string) string {
	if !strings.Contains(f, "%v") {
		return f
	}
	var b strings.Builder
	b.Grow(len(f))
	for i := 0; i < len(f); i++ {
		if f[i] != '%' || i+1 == len(f) {
			b.WriteByte(f[i])
			continue
		}
		switch f[i+1] {
		case '%':
			b.WriteString("%%")
		case 'v':
			b.WriteString("%s")
		default:
			b.WriteByte('%')
			continue
		}
		i++
	}
	return b.String()
}

// Translation represents a TranslationString with additional formatting, that
// may be filled out by calling F on it with a list of arguments for the
// translation.
type Translation struct {
	str      TranslationString
	format   string
	params   int
	fallback string
}

// Zero returns false if a Translation was not created using Translate or
// Untranslated.
func (t Translation) Zero() bool {
	return t.format == ""
}

// Enc encapsulates the translation string into the format passed. This format
// should have exactly one formatting identifier, %v, to specify where the
// translation string should go, such as 'translation: %v'.
// Enc accepts colouring formats parsed by text.Colourf.
func (t Translation) Enc(format string) Translation {
	t.format = format
	return t
}

// F takes arguments for a translation string passed and returns a filled out
// translation that may be sent to players. The number of arguments passed must
// be exactly equal to the number specified in Translate. If not, F will panic.
//
// Arguments are filled in as text: a text.Text as it is, a Translation that
// takes no arguments and a filled out translation as their text in the
// language of the reader, a TranslationString as what it resolves to, and
// any other value as fmt.Sprint formats it. Translations used as arguments
// should not require any parameters.
func (t Translation) F(a ...any) translation {
	if len(a) != t.params {
		panic(fmt.Sprintf("translation '%v' requires exactly %v parameters, got %v", t.format, t.params, len(a)))
	}
	return translation{t: t, params: a}
}

// text returns the text of t for the language l with the arguments passed,
// in the format of Enc: a translation of the key str resolves to, or, if it
// resolves to a string without a %, that string, read as Colourf reads an
// argument.
func (t Translation) text(l language.Tag, args []text.Text) text.Text {
	resolved := t.str.Resolve(l)
	key, ok := strings.CutPrefix(resolved, "%")
	if !ok {
		return text.Colourf(t.format, resolved)
	}
	return text.Colourf(t.format, text.Translate(key, t.fallback, args...))
}

// translation is a translation string with its arguments filled out. Text
// returns it in the language of a reader; String returns its fallback.
// translation implements Texter and the fmt.Stringer and error interfaces.
type translation struct {
	t      Translation
	params []any
}

// Text returns the translation as text for a reader of the language l: a
// translation that a client looks the key of up, with the arguments filled
// in, in the format of Enc. If its TranslationString resolves to a string
// without a %, it is that string in the format of Enc, and the arguments
// are not shown.
func (t translation) Text(l language.Tag) text.Text {
	args := make([]text.Text, len(t.params))
	for i, a := range t.params {
		args[i] = argText(a, l)
	}
	return t.t.text(l, args)
}

// argText returns the argument a of a translation as text for a reader of
// the language l. The arguments of a translation that is an argument are
// not filled in: a translation filled out with arguments is its key alone,
// as Bedrock Edition clients have always been sent it.
func argText(a any, l language.Tag) text.Text {
	switch v := a.(type) {
	case text.Text:
		return v
	case Translation:
		if v.params == 0 {
			return v.F().Text(l)
		}
		resolved := v.str.Resolve(l)
		if key, ok := strings.CutPrefix(resolved, "%"); ok {
			return text.Translate(key, v.fallback)
		}
		return text.Literal(resolved)
	case translation:
		if len(v.params) == 0 {
			return v.Text(l)
		}
		return v.t.text(l, nil)
	case TranslationString:
		return text.Literal(v.Resolve(l))
	}
	return text.Literal(fmt.Sprint(a))
}

// String formats and returns the fallback value of the translation, as a
// string with § codes. A translation passed as an argument, filled in or
// taking no arguments, is formatted as its own fallback, a text.Text as it
// is, and any other value as fmt.Sprint formats it.
func (t translation) String() string {
	args := make([]text.Text, len(t.params))
	for i, a := range t.params {
		args[i] = fallbackArg(a)
	}
	return text.Colourf(t.t.format, text.Translate("", t.t.fallback, args...)).Legacy()
}

// fallbackArg returns the argument a of a translation as its fallback shows
// it.
func fallbackArg(a any) text.Text {
	switch v := a.(type) {
	case text.Text:
		return v
	case Translation:
		if v.params == 0 {
			return text.Literal(v.F().String())
		}
	case translation:
		return text.Literal(v.String())
	}
	return text.Literal(fmt.Sprint(a))
}

// Error formats and returns the fallback value of the translation.
func (t translation) Error() string {
	return t.String()
}
