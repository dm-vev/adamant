package main

import (
	"fmt"
	"slices"
	"testing"

	bedrocktext "github.com/df-mc/dragonfly/protocol/bedrock/text"
	"github.com/df-mc/dragonfly/server/player/chat"
	"github.com/df-mc/dragonfly/server/text"
	nettext "github.com/dm-vev/adamant/net/bedrock/text"
	"golang.org/x/text/language"
)

// The texts of the benchmarks: a plain line, a coloured status line and a
// translation, the join message.
const (
	plainLine    = "Hello, world! How are you all doing today?"
	statusFormat = "<green>Online:</green> <white>%d</white><grey>/</grey><white>%d</white> <yellow>TPS %.1f</yellow>"
	joinName     = "Steve"
)

var statusArgs = []any{27, 100, 19.9}

// The old paths build the string sent with net/bedrock/text and pass it
// through, as the server did before text was structured; a translation
// was its key in the format of the message and its arguments printed with
// fmt.Sprint, as chat.Translation resolved it.

func oldPlain() string  { return nettext.Colourf(plainLine) }
func oldRaw() string    { return plainLine }
func oldStatus() string { return nettext.Colourf(statusFormat, statusArgs...) }
func oldTranslation() (string, []string) {
	return nettext.Colourf("<yellow>%v</yellow>", "%multiplayer.player.joined"), []string{fmt.Sprint(joinName)}
}

// The new paths build structured text with server/text and render it with
// protocol/bedrock/text; the Legacy ones read a string that carries §
// codes, as a Player reads the strings of its text API.

func newPlain() string        { return bedrocktext.String(text.Colourf(plainLine)) }
func newPlainLegacy() string  { return bedrocktext.String(text.ParseLegacy(plainLine)) }
func newStatus() string       { return bedrocktext.String(text.Colourf(statusFormat, statusArgs...)) }
func newStatusLegacy() string { return bedrocktext.String(text.ParseLegacy(oldStatus())) }
func newTranslation() (string, []string) {
	m, params, _ := bedrocktext.Message(chat.MessageJoin.F(joinName).Text(language.English))
	return m, params
}

// TestTextPathsAgree checks that the old and the new paths send the same
// strings, so that the benchmarks compare like with like.
func TestTextPathsAgree(t *testing.T) {
	for _, c := range []struct {
		name     string
		old, new string
	}{
		{"plain", oldPlain(), newPlain()},
		{"plain legacy", oldRaw(), newPlainLegacy()},
		{"status", oldStatus(), newStatus()},
		{"status legacy", oldStatus(), newStatusLegacy()},
	} {
		if c.old != c.new {
			t.Errorf("%s: old %q, new %q", c.name, c.old, c.new)
		}
	}
	om, op := oldTranslation()
	nm, np := newTranslation()
	if om != nm || !slices.Equal(op, np) {
		t.Errorf("translation: old %q %q, new %q %q", om, op, nm, np)
	}
}

var sink string

func BenchmarkTextPlain(b *testing.B) {
	b.Run("old", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sink = oldPlain()
		}
	})
	b.Run("new", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sink = newPlain()
		}
	})
	// A string without codes, passed through before and read as legacy
	// text now.
	b.Run("old-raw", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sink = oldRaw()
		}
	})
	b.Run("new-legacy", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sink = newPlainLegacy()
		}
	})
}

func BenchmarkTextStatus(b *testing.B) {
	b.Run("old", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sink = oldStatus()
		}
	})
	b.Run("new", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sink = newStatus()
		}
	})
	b.Run("new-legacy", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sink = newStatusLegacy()
		}
	})
}

func BenchmarkTextTranslation(b *testing.B) {
	b.Run("old", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sink, _ = oldTranslation()
		}
	})
	b.Run("new", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sink, _ = newTranslation()
		}
	})
}
