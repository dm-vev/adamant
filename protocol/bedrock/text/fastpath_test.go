package bedrocktext

import (
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/text"
	nettext "github.com/dm-vev/adamant/net/bedrock/text"
)

// Message and Inline write a text that holds no translation as String does,
// and String writes a text ParseLegacy returned as the string it was read
// from, without walking it. oldMessage and oldInline are Message and Inline
// as they were before, walking every text, and oldAppend is String as it
// was: FuzzLegacyFastPath holds the renderers to them.

// oldAppendSpan appends the reset and the codes of s to dst, and its text.
func oldAppendSpan(dst []byte, s text.Span) []byte {
	if s.Reset {
		dst = append(dst, nettext.Reset...)
	}
	for _, c := range s.Codes {
		dst = append(dst, c.Legacy()...)
	}
	return append(dst, s.Text...)
}

// oldMessage is Message as it was.
func oldMessage(t text.Text) (message string, params []string, translated bool) {
	var dst []byte
	for s := range t.Spans(text.SpanOptions{Decorations: decorations, Translations: all}) {
		dst = oldAppendSpan(dst, s)
		if s.Translation.IsZero() {
			continue
		}
		translated = true
		key, _, _ := s.Translation.Translation()
		dst = append(dst, '%')
		dst = append(dst, key...)
		for arg := range s.Translation.Args() {
			params = append(params, oldInline(arg))
		}
	}
	return string(dst), params, translated
}

// oldInline is Inline as it was.
func oldInline(t text.Text) string {
	var dst []byte
	for s := range t.Spans(text.SpanOptions{Decorations: decorations, Translations: withoutArgs}) {
		dst = oldAppendSpan(dst, s)
		if !s.Translation.IsZero() {
			key, _, _ := s.Translation.Translation()
			dst = append(dst, '%')
			dst = append(dst, key...)
		}
	}
	return string(dst)
}

// checkRenderers fails t if String, Append, Message or Inline write tr
// otherwise than the renderers did before they wrote some texts without
// walking them.
func checkRenderers(t testing.TB, what string, tr text.Text) {
	t.Helper()
	want := string(oldAppend(nil, tr))
	if got := String(tr); got != want {
		t.Fatalf("%s: String(%#v) = %q, walked %q", what, tr, got, want)
	}
	if got := Append([]byte("x"), tr); string(got) != "x"+want {
		t.Fatalf("%s: Append of %#v = %q, walked %q", what, tr, got, "x"+want)
	}
	msg, params, translated := Message(tr)
	wantMsg, wantParams, wantTranslated := oldMessage(tr)
	if msg != wantMsg || !slices.Equal(params, wantParams) || (params == nil) != (wantParams == nil) || translated != wantTranslated {
		t.Fatalf("%s: Message(%#v) = %q, %q, %v, walked %q, %q, %v", what, tr, msg, params, translated, wantMsg, wantParams, wantTranslated)
	}
	if got, want := Inline(tr), oldInline(tr); got != want {
		t.Fatalf("%s: Inline(%#v) = %q, walked %q", what, tr, got, want)
	}
}

// checkFastPath fails t if the text ParseLegacy reads from s, or a text
// built from it, renders otherwise than it does walked: the text itself as
// s, as a copy that does not know s, and as the old renderers write it.
func checkFastPath(t testing.TB, s, arg string) {
	t.Helper()
	p := text.ParseLegacy(s)
	slow := p.Append()
	if !text.Equal(p, slow) {
		t.Fatalf("ParseLegacy(%q).Append() = %#v, not equal to %#v", s, slow, p)
	}
	if got := String(p); got != s {
		t.Fatalf("String(ParseLegacy(%q)) = %q", s, got)
	}
	if got := string(oldAppend(nil, slow)); got != s {
		t.Fatalf("ParseLegacy(%q) walked renders as %q", s, got)
	}
	checkRenderers(t, "ParseLegacy", p)
	checkRenderers(t, "copy", slow)
	a := text.ParseLegacy(arg)
	red := text.Style{Colour: text.Red}
	for what, d := range map[string]text.Text{
		"same style":      p.WithStyle(p.Style()),
		"red":             p.WithStyle(red),
		"reset":           p.WithStyle(text.Style{Reset: true, On: text.Bold}),
		"off":             text.Literal("x").WithStyle(text.Style{On: text.Bold}).Append(p.WithStyle(text.Style{Off: text.Bold})),
		"appended":        p.Append(a, text.Literal(arg)),
		"concat":          text.Concat(a, p, a),
		"child":           text.Literal("x").WithStyle(red).Append(p),
		"argument":        text.Translate("k", "%s!", p, a),
		"no fallback":     text.Translate("k", "", p),
		"inline arg":      text.Translate("k", "%s", text.Concat(p, text.Translate("j", "J"))),
		"translation":     text.Concat(p, text.Translate("k", "f")),
		"styled argument": text.Translate("k", "%2$s %1$s", p.WithStyle(red), a).WithStyle(text.Style{On: text.Italic}),
	} {
		checkRenderers(t, what, d)
	}
}

// FuzzLegacyFastPath compares the renderers, which write a text ParseLegacy
// returned as the string it was read from and a text with no translation
// as String writes it, with the renderers that walked every text, for the
// text of every string, the texts built from it, and the random trees of
// FuzzTree.
func FuzzLegacyFastPath(f *testing.F) {
	for _, in := range corpus(f, "FuzzLegacy") {
		f.Add(in[0].(string), "", []byte{}, false)
	}
	for _, in := range corpus(f, "FuzzTree") {
		f.Add("", "", in[0].([]byte), in[1].(bool))
	}
	for in, want := range colourfCases {
		f.Add(in, want, []byte{}, false)
	}
	for _, s := range legacyFragments {
		f.Add("a"+s+"§c"+s+"§r", s, []byte{}, true)
	}
	f.Add("§6Coins: §e1200 §7| §aOnline: §f42 §7| §bPing: §f35ms", "§r§l", []byte{3, 0, 120, 6, 2, 3, 0, 1}, true)
	f.Fuzz(func(t *testing.T, s, arg string, data []byte, bedrock bool) {
		checkFastPath(t, s, arg)
		checkFastPath(t, arg, s)
		checkRenderers(t, "tree", (&treeReader{data: data, bedrock: bedrock}).text(5))
	})
}

func TestLegacyFastPath(t *testing.T) {
	for _, s := range legacyFragments {
		checkFastPath(t, s, "a"+s+"§c"+s+"§r")
	}
	for in, want := range colourfCases {
		checkFastPath(t, in, want)
	}
}

func TestLegacyFastPathAllocations(t *testing.T) {
	p := text.ParseLegacy("§6Coins: §e1200 §7| §aOnline: §f42 §7| §bPing: §f35ms")
	if n := testing.AllocsPerRun(100, func() { _ = String(p) }); n != 0 {
		t.Errorf("String allocates %v times, want none", n)
	}
	if n := testing.AllocsPerRun(100, func() { _, _, _ = Message(p) }); n != 0 {
		t.Errorf("Message allocates %v times, want none", n)
	}
	if n := testing.AllocsPerRun(100, func() { _ = Inline(p) }); n != 0 {
		t.Errorf("Inline allocates %v times, want none", n)
	}
}
