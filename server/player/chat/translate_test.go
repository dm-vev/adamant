package chat

import (
	"io"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/text"
	"golang.org/x/text/language"
)

func TestLiteralTranslationFallback(t *testing.T) {
	for _, literal := range []string{"100% complete", "Literal %s and %v", "Сохранено 100%"} {
		translated := Translate(str("unused.key"), 0, literal).F()
		if got := text.ParseLegacy(translated.String()).Plain(); got != literal {
			t.Errorf("literal fallback = %q, want %q", got, literal)
		}
	}
	// Parameterized translations still interpret their fallback format.
	if got := text.ParseLegacy(Translate(str("unused.key"), 1, "Value: %v").F(42).String()).Plain(); got != "Value: 42" {
		t.Fatalf("parameterized fallback = %q", got)
	}
}

func TestTranslationArgumentFallback(t *testing.T) {
	stone := Translate(str("%tile.stone.name"), 0, "Stone").Enc("<grey>%v</grey>")
	mob := Translate(str("%death.attack.mob"), 2, "%v was slain by %v")
	for _, c := range []struct {
		name string
		arg  any
		want string
	}{
		{"a translation that takes no arguments", stone, "Steve was slain by Stone"},
		{"a translation filled in", stone.F(), "Steve was slain by Stone"},
		{"a translation filled in with arguments", MessageJoin.F("Alex"), "Steve was slain by Alex joined the game"},
	} {
		if got := text.ParseLegacy(mob.F("Steve", c.arg).String()).Plain(); got != c.want {
			t.Errorf("%s: fallback %q, want %q", c.name, got, c.want)
		}
	}
	if got, want := mob.F("Steve", stone).String(), "§rSteve was slain by §r§7Stone§r"; got != want {
		t.Errorf("fallback %q, want %q", got, want)
	}
}

func TestTranslateFallbacks(t *testing.T) {
	for _, c := range []struct {
		params   int
		fallback string
		want     string
	}{
		{0, "100% done", "100%% done"},
		{0, "%v and %s", "%%v and %%s"},
		{1, "%v joined", "%s joined"},
		{1, "100%% of %v", "100%% of %s"},
		{1, "%%v is %v", "%%v is %s"},
		{2, "%v%v", "%s%s"},
		{1, "%s and %d and %", "%s and %d and %"},
		{1, "end %", "end %"},
	} {
		if got := Translate(str("k"), c.params, c.fallback).fallback; got != c.want {
			t.Errorf("fallback %q with %d parameters is stored as %q, want %q", c.fallback, c.params, got, c.want)
		}
	}
}

func TestTranslationText(t *testing.T) {
	var texter Texter = MessageJoin.F("§bSteve")
	want := text.Colourf("<yellow>%v</yellow>", text.Translate("multiplayer.player.joined", "%s joined the game", text.Literal("§bSteve")))
	if got := texter.Text(language.English); !text.Equal(got, want) {
		t.Errorf("Text = %#v, want %#v", got, want)
	}
	if got, want := MessageJoin.F("§bSteve").String(), "§r§e§bSteve joined the game§r"; got != want {
		t.Errorf("String = %q, want %q", got, want)
	}
	disconnect := text.Colourf("<yellow>%v</yellow>", text.Translate("disconnect.disconnected", "Disconnected by Server"))
	if got := MessageServerDisconnect.F().Text(language.English); !text.Equal(got, disconnect) {
		t.Errorf("Text = %#v, want %#v", got, disconnect)
	}
}

func TestStdoutSubscriber(t *testing.T) {
	for _, c := range []struct {
		a    []any
		want string
	}{
		{[]any{"§cRed", 3}, "\x1b[38;5;203mRed 3\n"},
		{[]any{"line\n"}, "line\n"},
		{[]any{text.Literal("x").WithStyle(text.Style{Colour: text.Red})}, "\x1b[38;5;203mx\n"},
	} {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		stdout := os.Stdout
		os.Stdout = w
		StdoutSubscriber{}.Message(c.a...)
		os.Stdout = stdout
		_ = w.Close()
		b, _ := io.ReadAll(r)
		if string(b) != c.want {
			t.Errorf("Message(%q) printed %q, want %q", c.a, b, c.want)
		}
	}
}
