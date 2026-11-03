package text

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// legacy writes t as a stream of § codes with the codes of ParseLegacy, a
// renderer for the tests of this package: protocol/bedrock/text renders
// text for the game.
func legacy(t Text) string {
	chars := map[Code]byte{}
	for c := 0; c < len(legacyCodes); c++ {
		if code := legacyCodes[c]; code.ok && !code.reset {
			chars[code.code] = byte(c)
		}
	}
	var b strings.Builder
	for s := range t.Spans(SpanOptions{Decorations: Obfuscated | Bold | Italic}) {
		if s.Reset {
			b.WriteString("§r")
		}
		for _, c := range s.Codes {
			if c.Colour != NoColour {
				c.Colour = c.Colour.Nearest()
			}
			b.WriteString("§" + string(rune(chars[c])))
		}
		b.WriteString(s.Text)
		if !s.Translation.IsZero() {
			key, _, _ := s.Translation.Translation()
			b.WriteString("%" + key)
		}
	}
	return b.String()
}

func TestColourf(t *testing.T) {
	for _, c := range []struct {
		in, tree, legacy string
	}{
		// The cases of the tests of net/bedrock/text.
		{"<red>Hello <bold>World</bold>!</red>", `{reset {red "Hello "} {red {+bold "World"}} {red "!"} {""}}`, "§r§cHello §r§c§lWorld§r§c!§r"},
		{"plain", `{reset {"plain"}}`, "§rplain"},
		{"<unknown>x</unknown>", `{reset {"<unknown>x</unknown>"}}`, "§r<unknown>x</unknown>"},
		{"a &amp; b <!-- note -->c", `{reset {"a & b c"}}`, "§ra & b c"},
		{"", `{}`, ""},
		// Tags only, and tags closed out of order or not at all.
		{"<!-- c -->", `{reset}`, "§r"},
		{"<red></red>", `{reset}`, "§r"},
		{"<red><blue><red>x</red>y", `{reset {red {blue {red "x"}}} {blue {red "y"}} {""}}`, "§r§c§9§cx§r§9§cy§r"},
		{"<b>a</bold>b", `{reset {+bold "a"} {"b"}}`, "§r§la§rb"},
		{"<RED class='x'>a</Red>", `{reset {red "a"} {""}}`, "§r§ca§r"},
		{"<i><obfuscated>x", `{reset {+italic {+obfuscated "x"}} {""}}`, "§r§o§kx§r"},
		{"</red>x", `{reset {"</red>x"}}`, "§r</red>x"},
		// Raw elements keep their content.
		{"<script><red>x</red></script>", `{reset {"<script><red>x</red></script>"}}`, "§r<script><red>x</red></script>"},
		{"<red><title>&lt;</title></red>", `{reset {red "<title>"} {red "<"} {red "</title>"} {""}}`, "§r§c<title>§r§c<§r§c</title>§r"},
		// § codes in the text stay text.
		{"<red>§lx</red>", `{reset {red "§lx"} {""}}`, "§r§c§lx§r"},
	} {
		got := Colourf("%s", c.in)
		if s := fmt.Sprintf("%#v", got); s != c.tree {
			t.Errorf("Colourf(%q) = %s, want %s", c.in, s, c.tree)
		}
		if s := legacy(got); s != c.legacy {
			t.Errorf("Colourf(%q) writes %q, want %q", c.in, s, c.legacy)
		}
	}
	if got := Colourf("<red>%v</red>", 3); fmt.Sprintf("%#v", got) != `{reset {red "3"} {""}}` {
		t.Errorf("formatted %#v", got)
	}
	// Markup in arguments is read.
	if got := Colourf("<red>%v</red>", "<b>x</b>"); legacy(got) != "§r§c§lx§r" {
		t.Errorf("markup in an argument: %#v", got)
	}
}

func TestColourfText(t *testing.T) {
	name := Literal("Ste<ve>").WithStyle(Style{Colour: Aqua})
	got := Colourf("<yellow>%v joined</yellow> %s", name, Literal("&amp;"))
	if want := `{reset {yellow {aqua "Ste<ve>"} {" joined"}} {" &amp;"}}`; fmt.Sprintf("%#v", got) != want {
		t.Errorf("texts as arguments: %#v, want %s", got, want)
	}
	if s := legacy(got); s != "§r§e§bSte<ve>§r§e joined§r &amp;" {
		t.Errorf("texts as arguments write %q", s)
	}
	// Other verbs format the plain text, and texts in tags or comments are
	// dropped with them.
	if got := Colourf("%q|%5s|<a %v>|<!-- %v -->", name, name, name, name); got.Plain() != `"Ste<ve>"|Ste<ve>|<a>|` {
		t.Errorf("verbs: %#v", got)
	}
	// A format that holds the marks of the texts formats them as text.
	if got := Colourf("\uFDD0%v", Literal("<red>x")); fmt.Sprintf("%#v", got) != `{reset {"\ufdd0"} {red "x"} {""}}` {
		t.Errorf("marks in the format: %#v", got)
	}
	if got := Colourf("%v%v", Literal("a"), "\uFDD00\uFDD1"); got.Plain() != "a\uFDD00\uFDD1" {
		t.Errorf("marks in an argument: %#v", got)
	}
	// A text placed twice, and the zero Text, which is formatted as "" is.
	if got := Colourf("%[1]v<red>%[1]v</red>%v", Literal("x"), Text{}); fmt.Sprintf("%#v", got) != `{reset {"x"} {red {"x"}} {""}}` {
		t.Errorf("a text placed twice: %#v", got)
	}
	if got, want := Colourf("<red>a</red>%v", Text{}), Colourf("<red>a</red>%v", ""); !Equal(got, want) {
		t.Errorf("the zero Text: %#v, want %#v", got, want)
	}
}

func TestParseLegacy(t *testing.T) {
	for _, c := range []struct{ in, tree string }{
		{"", `{}`},
		{"plain", `{"plain"}`},
		{"§r", `{reset}`},
		{"§r§r", `{reset {reset}}`},
		{"§cRed §lbold§r §zkept", `{{red "Red " {+bold "bold"}} {reset " §zkept"}}`},
		{"a§cb§c§r", `{"a" {red "b" {red ""}} {reset}}`},
		{"§r§cA§r§c§lB§r§c!§r", `{reset {red "A"} {reset red {+bold "B"}} {reset red "!"} {reset}}`},
		{"§c", `{{red ""}}`},
		{"§", `{"§"}`},
		{"§§c§C", `{"§" {red "§C"}}`},
		{"x§r§k", `{"x" {reset +obfuscated ""}}`},
		{"§m§n§g§v", `{{redstone {copper {dark-yellow {resin ""}}}}}`},
	} {
		got := ParseLegacy(c.in)
		if s := fmt.Sprintf("%#v", got); s != c.tree {
			t.Errorf("ParseLegacy(%q) = %s, want %s", c.in, s, c.tree)
		}
		if s := legacy(got); s != c.in {
			t.Errorf("ParseLegacy(%q) writes %q", c.in, s)
		}
	}
	if got := ParseLegacy("§cRed §lbold§r §zkept").Plain(); got != "Red bold §zkept" {
		t.Errorf("Plain = %q", got)
	}
}

func TestSpans(t *testing.T) {
	red := Style{Colour: Red}
	for _, c := range []struct {
		name string
		t    Text
		want string
	}{
		{"siblings share codes", Concat(Literal("a").WithStyle(red).Append(Literal("b").WithStyle(Style{On: Bold}), Literal("c"))), "§ca§lb§r§cc"},
		{"a parent's codes are written once", Literal("a").WithStyle(red).Append(Literal("b"), Literal("c")), "§cabc"},
		{"a text with no codes needs no reset", Concat(Literal("a").WithStyle(Style{On: Underlined}), Literal("b")), "ab"},
		{"turning off resets", Literal("a").WithStyle(Style{Colour: Red, On: Bold | Italic}).Append(Literal("b").WithStyle(Style{Off: Bold}), Literal("c")), "§c§l§oa§r§c§ob§r§c§l§oc"},
		{"turning off what is off does nothing", Literal("a").WithStyle(red).Append(Literal("b").WithStyle(Style{Off: Bold})), "§cab"},
		{"turning off with nothing written", Literal("").WithStyle(Style{On: Bold}).Append(Literal("b").WithStyle(Style{Off: Bold, Colour: Blue})), "§l§r§9b"},
		{"a reset within", Literal("a").WithStyle(red).Append(Literal("b").WithStyle(Style{Reset: true}), Literal("c")), "§ca§rb§cc"},
		{"a reset with codes within", Literal("a").WithStyle(red).Append(Literal("b").WithStyle(Style{Reset: true, On: Bold}), Literal("c")), "§ca§r§lb§r§cc"},
		{"an empty text writes codes", Concat(Literal("a").WithStyle(red), Literal("")), "§ca§r"},
		{"a text with no content writes nothing", Concat(Text{}.WithStyle(red), Literal("b")), "b"},
		{"an RGB colour", Literal("a").WithStyle(Style{Colour: RGB(0xFF, 0x60, 0x60)}), "§ca"},
		{"a translation filled in", Translate("k", "<%s|%2$s|%%|%1$s>", Literal("x").WithStyle(red), Literal("y")).WithStyle(Style{On: Bold}), "§l<§cx§r§l|y|%|§cx§r§l>"},
	} {
		if got := legacy(c.t); got != c.want {
			t.Errorf("%s: %#v writes %q, want %q", c.name, c.t, got, c.want)
		}
	}

	// Translations as spans of their own.
	tr := Concat(Literal("a").WithStyle(red), Translate("k", "f", Literal("x")).WithStyle(Style{Colour: Blue}))
	var got []string
	for s := range tr.Spans(SpanOptions{Decorations: AllDecorations, Translations: func(Text) bool { return true }}) {
		got = append(got, fmt.Sprintf("%v %v %q %#v", s.Reset, s.Codes, s.Text, s.Translation))
	}
	want := []string{`false [{red none}] "a" {}`, `true [{blue none}] "" {blue tr("k", "f", {"x"})}`}
	if !slices.Equal(got, want) {
		t.Errorf("translation spans = %q, want %q", got, want)
	}

	// The consumer may stop at any span.
	n := 0
	for range Colourf("<red>a</red>b<red>c</red>").Spans(SpanOptions{}) {
		n++
		if n == 2 {
			break
		}
	}
	if n != 2 {
		t.Errorf("stopped after %d spans", n)
	}
}

func TestTranslate(t *testing.T) {
	x, y := Literal("x"), Literal("y")
	for _, c := range []struct {
		fallback string
		args     []Text
		want     string
	}{
		{"", nil, "key"},
		{"%s and %s", []Text{x, y}, "x and y"},
		{"%2$s, %1$s, %s", []Text{x, y}, "y, x, x"},
		{"100%% done", nil, "100% done"},
		{"100% done", nil, "100% done"},
		{"%s", nil, "%s"},
		{"%3$s", []Text{x, y}, "%3$s"},
		{"%0$s", []Text{x}, "%0$s"},
		{"%d", []Text{x}, "%d"},
		{"%v", []Text{x}, "%v"},
		{"%", nil, "%"},
		{"a%1$%", []Text{x}, "a%1$%"},
		{"%s|%s", []Text{x, {}}, "x|"},
		{"%99999999999999999999$s", []Text{x}, "%99999999999999999999$s"},
	} {
		if got := Translate("key", c.fallback, c.args...).Plain(); got != c.want {
			t.Errorf("fallback %q with %d arguments = %q, want %q", c.fallback, len(c.args), got, c.want)
		}
	}
	tr := Translate("key", "fallback", x, y)
	if key, fallback, ok := tr.Translation(); !ok || key != "key" || fallback != "fallback" {
		t.Errorf("Translation() = %q, %q, %v", key, fallback, ok)
	}
	if args := slices.Collect(tr.Args()); len(args) != 2 || !Equal(args[0], x) || !Equal(args[1], y) {
		t.Errorf("Args() = %#v", args)
	}
	if _, _, ok := x.Translation(); ok {
		t.Error("a literal is a translation")
	}
}

func TestImmutable(t *testing.T) {
	args := []Text{Literal("x")}
	tr := Translate("k", "%s", args...)
	args[0] = Literal("changed")
	if tr.Plain() != "x" {
		t.Errorf("Translate keeps the slice of its arguments")
	}

	base := Literal("a").Append(Literal("b"))
	styled := base.WithStyle(Style{Colour: Red})
	more := base.Append(Literal("c"))
	other := base.Append(Literal("d"))
	if base.Style() != (Style{}) || base.Plain() != "ab" || more.Plain() != "abc" || other.Plain() != "abd" || styled.Plain() != "ab" {
		t.Errorf("texts changed: %#v %#v %#v %#v", base, styled, more, other)
	}

	parts := []Text{Literal("a"), Literal("b")}
	c := Concat(parts...)
	parts[0] = Literal("z")
	if c.Plain() != "ab" {
		t.Errorf("Concat keeps the slice of its parts")
	}
}

func TestConstructors(t *testing.T) {
	if Literal("").IsZero() {
		t.Error("an empty literal is zero")
	}
	if !Concat().IsZero() || !Concat(Text{}, Text{}).IsZero() {
		t.Error("Concat of nothing is not zero")
	}
	if x := Literal("x"); !Equal(Concat(Text{}, x), x) {
		t.Error("Concat of one text is not the text")
	}
	if got := fmt.Sprintf("%#v", Text{}.Append(Literal("a"), Literal("b"))); got != `{{"a"} {"b"}}` {
		t.Errorf("Append to the zero Text = %s", got)
	}
	if got := fmt.Sprintf("%#v", Text{}.WithStyle(Style{Colour: Red})); got != `{red}` {
		t.Errorf("WithStyle on the zero Text = %s", got)
	}
	s := Literal("x").WithStyle(Style{Colour: Colour(1000), On: Bold | 0x80, Off: Bold | Italic}).Style()
	if s != (Style{On: Bold, Off: Italic}) {
		t.Errorf("WithStyle normalised to %+v", s)
	}
	if s := Literal("x").WithStyle(Style{Reset: true, Off: Italic}).Style(); s != (Style{Reset: true}) {
		t.Errorf("WithStyle with Reset normalised to %+v", s)
	}
	if l, ok := Literal("x").Literal(); !ok || l != "x" {
		t.Errorf("Literal() = %q, %v", l, ok)
	}
	if _, ok := Concat(Literal("a"), Literal("b")).Literal(); ok {
		t.Error("a text of no content is literal")
	}
	if got := Literal("a").Append(Literal("b"), Literal("c")).String(); got != "abc" {
		t.Errorf("String() = %q", got)
	}
}

func TestEqual(t *testing.T) {
	red := Style{Colour: Red}
	a := Colourf("<red>a</red>%v", Translate("k", "f", Literal("x")))
	b := Colourf("<red>a</red>%v", Translate("k", "f", Literal("x")))
	if !Equal(a, b) || !Equal(Text{}, Text{}) {
		t.Error("equal texts differ")
	}
	for _, other := range []Text{
		{},
		Colourf("<red>a</red>%v", Translate("k", "g", Literal("x"))),
		Colourf("<red>a</red>%v", Translate("k", "f", Literal("y"))),
		Colourf("<red>a</red>%v", Translate("k", "f")),
		Colourf("<red>a</red>%v", Translate("j", "f", Literal("x"))),
		Colourf("<blue>a</blue>%v", Translate("k", "f", Literal("x"))),
		Colourf("<red>a</red>"),
	} {
		if Equal(a, other) {
			t.Errorf("%#v equals %#v", a, other)
		}
	}
	// Texts that look the same.
	same := [][2]Text{
		{Literal("ab").WithStyle(red), Concat(Literal("a"), Literal("b")).WithStyle(red)},
		{ParseLegacy("§cA§r§cB"), Literal("AB").WithStyle(red)},
		{Colourf("<red>a</red>"), ParseLegacy("§ca")},
		{Literal("a").WithStyle(Style{Colour: Red, On: Bold}).Append(Literal("b").WithStyle(Style{Off: Bold})), Concat(Literal("a").WithStyle(Style{Colour: Red, On: Bold}), Literal("b").WithStyle(red))},
		{Translate("k", "f", Literal("x")).WithStyle(red), Concat(Translate("k", "f", Literal("x").WithStyle(red))).WithStyle(red)},
	}
	for _, p := range same {
		if !Equivalent(p[0], p[1]) {
			t.Errorf("%#v is not equivalent to %#v", p[0], p[1])
		}
		if Equal(p[0], p[1]) {
			t.Errorf("%#v equals %#v", p[0], p[1])
		}
	}
	differ := [][2]Text{
		{Literal("a").WithStyle(red), Literal("a")},
		{Literal("a").WithStyle(red), Literal("a").WithStyle(Style{Colour: Red, On: Bold})},
		{Translate("k", "f"), Literal("f")},
		{Translate("k", "f", Literal("x").WithStyle(red)), Translate("k", "f", Literal("x"))},
		{ParseLegacy("§ca§rb"), ParseLegacy("§cab")},
	}
	for _, p := range differ {
		if Equivalent(p[0], p[1]) {
			t.Errorf("%#v is equivalent to %#v", p[0], p[1])
		}
	}
}

func TestANSI(t *testing.T) {
	const (
		reset = "\x1b[m"
		red   = "\x1b[38;5;203m"
		bold  = "\x1b[1m"
	)
	for _, c := range []struct {
		t    Text
		want string
	}{
		{Colourf("<red>Hello <bold>World</bold>!</red>"), reset + red + "Hello " + reset + red + bold + "World" + reset + red + "!" + reset},
		{ParseLegacy("§cRed §kx§r§z"), red + "Red x" + reset + "§z"},
		// Codes in literal text, also across texts.
		{Concat(Literal("§"), Literal("c"), Literal("x§")), red + "x§"},
		{Literal("x").WithStyle(Style{Colour: RGB(0xFF, 0x50, 0x50), On: Underlined}), red + "x"},
		{Translate("k", "%s!", Literal("x").WithStyle(Style{Colour: Red})), red + "x" + reset + "!"},
		{Text{}, ""},
	} {
		if got := c.t.ANSI(); got != c.want {
			t.Errorf("%#v.ANSI() = %q, want %q", c.t, got, c.want)
		}
	}
}

func TestColour(t *testing.T) {
	if Red.String() != "red" || DarkYellow.String() != "dark-yellow" || NoColour.String() != "none" || Colour(99).String() != "none" {
		t.Error("names of colours")
	}
	if got := RGB(0x01, 0x02, 0xA3).String(); got != "#0102a3" {
		t.Errorf("RGB name = %q", got)
	}
	if r, g, b := Gold.RGB(); r != 0xFF || g != 0xBF || b != 0x1E {
		t.Errorf("Gold.RGB() = %x %x %x", r, g, b)
	}
	if r, g, b := RGB(1, 2, 3).RGB(); r != 1 || g != 2 || b != 3 {
		t.Errorf("RGB(1, 2, 3).RGB() = %v %v %v", r, g, b)
	}
	if !Red.Named() || RGB(0xFF, 0x55, 0x55).Named() || !RGB(0xFF, 0x55, 0x55).IsRGB() || Red.IsRGB() || NoColour.Named() {
		t.Error("kinds of colours")
	}
	for c := Black; c <= lastNamed; c++ {
		r, g, b := c.RGB()
		if n := RGB(r, g, b).Nearest(); n != c {
			t.Errorf("the colour nearest to the RGB of %v is %v", c, n)
		}
		if c.Nearest() != c {
			t.Errorf("%v.Nearest() = %v", c, c.Nearest())
		}
	}
	if NoColour.Nearest() != NoColour || Colour(99).Nearest() != NoColour {
		t.Error("Nearest of no colour")
	}
	if got := (Bold | Italic).String(); got != "bold|italic" {
		t.Errorf("decorations = %q", got)
	}
	if got := Decoration(0).String(); got != "none" {
		t.Errorf("no decorations = %q", got)
	}
}

func TestChildren(t *testing.T) {
	x := Literal("x").Append(Literal("a"), Literal("b"))
	if got := slices.Collect(x.Children()); len(got) != 2 || got[0].Plain() != "a" || got[1].Plain() != "b" {
		t.Errorf("Children() = %#v", got)
	}
	for range x.Children() {
		break
	}
	if len(slices.Collect(Text{}.Children())) != 0 || len(slices.Collect(Text{}.Args())) != 0 {
		t.Error("the zero Text has children")
	}
}
