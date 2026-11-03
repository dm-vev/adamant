package chat

import (
	"testing"

	"github.com/dm-vev/adamant/net/bedrock/text"
)

func TestLiteralTranslationFallback(t *testing.T) {
	for _, literal := range []string{"100% complete", "Literal %s and %v", "Сохранено 100%"} {
		translated := Translate(str("unused.key"), 0, literal).F()
		if got := text.Clean(translated.String()); got != literal {
			t.Errorf("literal fallback = %q, want %q", got, literal)
		}
	}
	// Parameterized translations still interpret their fallback format.
	if got := text.Clean(Translate(str("unused.key"), 1, "Value: %v").F(42).String()); got != "Value: 42" {
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
		if got := text.Clean(mob.F("Steve", c.arg).String()); got != c.want {
			t.Errorf("%s: fallback %q, want %q", c.name, got, c.want)
		}
	}
	if got, want := mob.F("Steve", stone).String(), "§rSteve was slain by §r§7Stone§r"; got != want {
		t.Errorf("fallback %q, want %q", got, want)
	}
}
