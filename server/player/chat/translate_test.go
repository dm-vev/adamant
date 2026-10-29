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
