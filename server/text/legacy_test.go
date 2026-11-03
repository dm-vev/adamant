package text

import (
	"math/rand/v2"
	"strings"
	"testing"
)

func TestCodeLegacy(t *testing.T) {
	for in, want := range map[Code]string{
		{Colour: Black}:                       "§0",
		{Colour: Red}:                         "§c",
		{Colour: White}:                       "§f",
		{Colour: DarkYellow}:                  "§g",
		{Colour: Redstone}:                    "§m",
		{Colour: Copper}:                      "§n",
		{Colour: Gold}:                        "§p",
		{Colour: Resin}:                       "§v",
		{Colour: RGB(0xFF, 0x50, 0x50)}:       "§c",
		{Colour: RGB(0xEE, 0x22, 0x2C)}:       "§m",
		{Colour: Red, Decoration: Bold}:       "§c",
		{Colour: Colour(99)}:                  "",
		{}:                                    "",
		{Decoration: Obfuscated}:              "§k",
		{Decoration: Bold}:                    "§l",
		{Decoration: Italic}:                  "§o",
		{Decoration: Underlined}:              "",
		{Decoration: Strikethrough}:           "",
		{Decoration: Bold | Italic}:           "",
		{Decoration: Obfuscated | Underlined}: "",
		{Decoration: 0x80}:                    "",
	} {
		if got := in.Legacy(); got != want {
			t.Errorf("%v.Legacy() = %q, want %q", in, got, want)
		}
	}
	// Every code ParseLegacy reads is written back as it was.
	for c := 0; c < len(legacyCodes); c++ {
		code := legacyCodes[c]
		if !code.ok || code.reset {
			continue
		}
		if got, want := code.code.Legacy(), "§"+string(rune(c)); got != want {
			t.Errorf("the code of §%c is written as %q", c, got)
		}
	}
	for c := Black; c <= lastNamed; c++ {
		if (Code{Colour: c}).Legacy() == "" {
			t.Errorf("%v has no code", c)
		}
	}
}

func TestLegacy(t *testing.T) {
	red, bold := Style{Colour: Red}, Style{On: Bold}
	for _, c := range []struct {
		name string
		t    Text
		want string
	}{
		{"the zero Text", Text{}, ""},
		{"a literal", Literal("a §c b"), "a §c b"},
		{"a style", Literal("a").WithStyle(red), "§ca"},
		{"no underline or strike-through", Concat(Literal("a").WithStyle(Style{On: Underlined | Strikethrough | Bold}), Literal("b")), "§la§rb"},
		{"an RGB colour", Literal("a").WithStyle(Style{Colour: RGB(0x55, 0x55, 0xFF)}), "§9a"},
		{"a reset", Literal("a").WithStyle(red).Append(Literal("b").WithStyle(Style{Reset: true}), Literal("c")), "§ca§rb§cc"},
		{"a translation filled in", Translate("k", "<%s|%%|%1$s>", Literal("x").WithStyle(red)).WithStyle(bold), "§l<§cx§r§l|%|§cx§r§l>"},
		{"a translation with no fallback", Translate("k", ""), "k"},
		{"a fallback that is not valid", Translate("k", "%d %s", Literal("x")), "%d %s"},
		{"markup", Colourf("<red>Hello <bold>World</bold>!</red>"), "§r§cHello §r§c§lWorld§r§c!§r"},
	} {
		if got := c.t.Legacy(); got != c.want {
			t.Errorf("%s: %#v.Legacy() = %q, want %q", c.name, c.t, got, c.want)
		}
		if got := string(c.t.AppendLegacy([]byte("x"))); got != "x"+c.want {
			t.Errorf("%s: AppendLegacy = %q", c.name, got)
		}
	}
}

// legacyPieces are what random legacy strings are made of.
var legacyPieces = func() []string {
	p := []string{"§", "\xc2", "\xa7", "a", "Hello ", "日本", "\xff", "%"}
	for c := byte(0x20); c < 0x7f; c++ {
		p = append(p, "§"+string(c))
	}
	return p
}()

func TestLegacyRoundTrip(t *testing.T) {
	r := rand.New(rand.NewPCG(7, 8))
	for range 20000 {
		var b strings.Builder
		for range 1 + r.IntN(12) {
			b.WriteString(legacyPieces[r.IntN(len(legacyPieces))])
		}
		s := b.String()
		if got := ParseLegacy(s).Legacy(); got != s {
			t.Fatalf("ParseLegacy(%q).Legacy() = %q", s, got)
		}
	}
}
