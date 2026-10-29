package interop

import (
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/dm-vev/adamant/net/bedrock/text"
	gttext "github.com/sandertv/gophertunnel/minecraft/text"
)

// textRuns is how many random texts a run compares.
var textRuns = 20000

// textFragments are the pieces random texts are made of: tags known and
// not, nested and not closed, references, brackets that open no tag and
// formatting codes.
var textFragments = []string{
	"<red>", "</red>", "<bold>", "</bold>", "<b>", "</b>", "<i>", "</i>", "<dark-aqua>", "</dark-aqua>",
	"<RED>", "</Red>", "<unknown>", "</unknown>", "<x-y>", "<red class=\"a\">", "Hello", " ", "world",
	"&amp;", "&lt;", "&gt;", "&#39;", "&quot;", "&nbsp;", "a < b", "<", ">", "< red>", "<1>", "§c", "§l", "§z",
	"§r", "%%", "日本", "\n", "<red/>", "<br/>", "<red class=\">\">", "</ red>", "</>", "<!-- c -->", "<!-->",
	"<!doctype x>", "</1>", "<?p>", "</red/>", "<script>", "</script>", "<title>", "</title>", "<style>", "<textarea>",
	"</textarea>", "<plaintext>", "<RÉD>", "</ÄB>", "<sCrIpT>", "</SCRIPT>", "\xff", "<a\xff>", "\x00", "<a\x00b>", "\r\n", "\t", "/", "-", "--", "->", "!", "?", "'", "\"", "&", "&am", ";", "<!", "</",
}

// TestTextMatchesGophertunnel formats random texts with the text package
// of the module and gophertunnel's, which Adamant used: Colourf, Clean and
// ANSI agree.
func TestTextMatchesGophertunnel(t *testing.T) {
	r := rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	for range textRuns {
		var b strings.Builder
		for range 1 + r.IntN(12) {
			b.WriteString(textFragments[r.IntN(len(textFragments))])
		}
		s := b.String()
		if got, want := text.Colourf("%s", s), gttext.Colourf("%s", s); got != want {
			t.Fatalf("Colourf(%q) = %q, gophertunnel %q", s, got, want)
		}
		if got, want := text.Clean(s), gttext.Clean(s); got != want {
			t.Fatalf("Clean(%q) = %q, gophertunnel %q", s, got, want)
		}
		if got, want := text.ANSI(s, 1), gttext.ANSI(s, 1); got != want {
			t.Fatalf("ANSI(%q) = %q, gophertunnel %q", s, got, want)
		}
	}
}
