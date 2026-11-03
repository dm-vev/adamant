package bedrocktext

import (
	"bufio"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/player/chat"
	"github.com/df-mc/dragonfly/server/text"
	nettext "github.com/dm-vev/adamant/net/bedrock/text"
	"golang.org/x/text/language"
)

// colourfCases are the cases of the tests of Colourf in net/bedrock/text,
// with the strings they expect.
var colourfCases = map[string]string{
	"<red>Hello <bold>World</bold>!</red>": "§r§cHello §r§c§lWorld§r§c!§r",
	"plain":                                "§rplain",
	"<unknown>x</unknown>":                 "§r<unknown>x</unknown>",
	"a &amp; b <!-- note -->c":             "§ra & b c",
	"":                                     "",
}

// netCorpus returns the seed corpus of FuzzColourf in net/bedrock/text:
// the text the fuzzer found that made Colourf panic, and the text the fuzz
// test adds.
func netCorpus(t testing.TB) []string {
	corpus := []string{"<red>a</red><script>x</script>&amp;<!-- c --></"}
	dir := filepath.Join("..", "..", "..", "net", "bedrock", "text", "testdata", "fuzz", "FuzzColourf")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read the corpus of net/bedrock/text: %v", err)
	}
	for _, e := range entries {
		f, err := os.Open(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := sc.Text()
			if v, ok := strings.CutPrefix(line, "string("); ok {
				s, err := strconv.Unquote(strings.TrimSuffix(v, ")"))
				if err != nil {
					t.Fatalf("%s: %v", e.Name(), err)
				}
				corpus = append(corpus, s)
			}
		}
		_ = f.Close()
	}
	if len(corpus) < 2 {
		t.Fatal("the corpus of net/bedrock/text is empty")
	}
	return corpus
}

// fragments are the pieces random markup is made of: those the tests of
// the net module compare net/bedrock/text with gophertunnel's text package
// with (net/interop/text_test.go), tags known and not, nested and not
// closed, references, brackets that open no tag and codes, and some more:
// every tag, and the marks Colourf places texts with.
var fragments = append([]string{
	"<red>", "</red>", "<bold>", "</bold>", "<b>", "</b>", "<i>", "</i>", "<dark-aqua>", "</dark-aqua>",
	"<RED>", "</Red>", "<unknown>", "</unknown>", "<x-y>", "<red class=\"a\">", "Hello", " ", "world",
	"&amp;", "&lt;", "&gt;", "&#39;", "&quot;", "&nbsp;", "a < b", "<", ">", "< red>", "<1>", "§c", "§l", "§z",
	"§r", "%%", "日本", "\n", "<red/>", "<br/>", "<red class=\">\">", "</ red>", "</>", "<!-- c -->", "<!-->",
	"<!doctype x>", "</1>", "<?p>", "</red/>", "<script>", "</script>", "<title>", "</title>", "<style>", "<textarea>",
	"</textarea>", "<plaintext>", "<RÉD>", "</ÄB>", "<sCrIpT>", "</SCRIPT>", "\xff", "<a\xff>", "\x00", "<a\x00b>", "\r\n", "\t", "/", "-", "--", "->", "!", "?", "'", "\"", "&", "&am", ";", "<!", "</",
	"\xef\xb7\x90", "\xef\xb7\x91", "0", "\r", "&#x41;", "&#0;", "<i ", "='", "\"x\"", "<p", "-->", "--!>", "<!---->",
}, tagFragments()...)

// tagFragments returns the opening and closing tags of every tag of
// Colourf.
func tagFragments() []string {
	var f []string
	for _, tag := range []string{
		"black", "dark-blue", "dark-green", "dark-aqua", "dark-red", "dark-purple", "orange", "grey", "dark-grey",
		"blue", "green", "aqua", "red", "purple", "yellow", "white", "dark-yellow", "quartz", "iron", "netherite",
		"obfuscated", "bold", "b", "redstone", "copper", "gold", "emerald", "italic", "i", "diamond", "lapis",
		"amethyst", "resin",
	} {
		f = append(f, "<"+tag+">", "</"+tag+">")
	}
	return f
}

// randomMarkup returns markup of up to n random fragments.
func randomMarkup(r *rand.Rand, n int) string {
	var b strings.Builder
	for range 1 + r.IntN(n) {
		b.WriteString(fragments[r.IntN(len(fragments))])
	}
	return b.String()
}

// checkColourf fails t if Colourf with the format and arguments passed does
// not render as Colourf of net/bedrock/text does, also through ANSI. It
// takes the arguments as a slice, so that vet does not hold a format that
// is not a constant against it.
func checkColourf(t testing.TB, format string, a []any) {
	t.Helper()
	got := text.Colourf(format, a...)
	want := nettext.Colourf(format, a...)
	if s := String(got); s != want {
		t.Fatalf("Colourf(%q, %q) = %q, want %q\ntext %#v", format, a, s, want, got)
	}
	if s := string(Append([]byte("x"), got)); s != "x"+want {
		t.Fatalf("Append of Colourf(%q, %q) = %q", format, a, s)
	}
	if s, ansi := got.ANSI(), nettext.ANSI(want); s != ansi {
		t.Fatalf("ANSI of Colourf(%q, %q) = %q, want %q", format, a, s, ansi)
	}
	if msg, params, translated := Message(got); msg != want || params != nil || translated {
		t.Fatalf("Message of Colourf(%q, %q) = %q, %q, %v", format, a, msg, params, translated)
	}
}

func TestColourfCases(t *testing.T) {
	for in, want := range colourfCases {
		if got := String(text.Colourf("%s", in)); got != want {
			t.Errorf("Colourf(%q) = %q, want %q", in, got, want)
		}
		checkColourf(t, "%s", []any{in})
		checkColourf(t, in, nil)
	}
	if got := String(text.Colourf("<red>%v</red>", 3)); got != "§r§c3§r" {
		t.Errorf("formatted %q", got)
	}
	checkColourf(t, "<red>%v</red>", []any{3})
	for _, s := range netCorpus(t) {
		checkColourf(t, "%s", []any{s})
		checkColourf(t, s, nil)
	}
}

func TestColourfMatches(t *testing.T) {
	seed := rand.Uint64()
	t.Logf("seed %d", seed)
	for _, r := range []*rand.Rand{rand.New(rand.NewPCG(1, 2)), rand.New(rand.NewPCG(seed, seed))} {
		for range 50000 {
			s := randomMarkup(r, 12)
			checkColourf(t, "%s", []any{s})
			checkColourf(t, s, nil)
			// Formatting verbs and arguments, markup in the arguments.
			checkColourf(t, "<red>%v</red>"+s+"%d %q", []any{randomMarkup(r, 3), r.IntN(100), randomMarkup(r, 3)})
			checkColourf(t, s+"%[2]s%[1]s%s %v %x", []any{randomMarkup(r, 3), randomMarkup(r, 3)})
		}
	}
}

// legacyFragments are the pieces random legacy strings are made of: every
// § code, § with every other ASCII character, parts of §, text, bytes that
// are not UTF-8 and runes of more than one byte.
var legacyFragments = func() []string {
	f := []string{"§", "\xc2", "\xa7", "\xc2\xc2\xa7", "a", "Hello ", " ", "日本", "\xff", "\x00", "\n", "%", "<red>", "&amp;"}
	for c := byte(0x20); c < 0x7f; c++ {
		f = append(f, "§"+string(c))
	}
	for c := byte('0'); c <= 'v'; c++ {
		if c <= '9' || c >= 'a' {
			f = append(f, "§"+string(c), "§"+string(c), "§r§"+string(c))
		}
	}
	return f
}()

// checkLegacy fails t if ParseLegacy(s) does not render as s, through
// String, Inline and Message, or does not show on the console as s does.
func checkLegacy(t testing.TB, s string) {
	t.Helper()
	p := text.ParseLegacy(s)
	if got := String(p); got != s {
		t.Fatalf("ParseLegacy(%q) renders as %q\ntext %#v", s, got, p)
	}
	if got := Inline(p); got != s {
		t.Fatalf("Inline(ParseLegacy(%q)) = %q", s, got)
	}
	if msg, params, translated := Message(p); msg != s || params != nil || translated {
		t.Fatalf("Message(ParseLegacy(%q)) = %q, %q, %v", s, msg, params, translated)
	}
	if got, want := p.Plain(), nettext.Clean(s); got != want {
		t.Fatalf("ParseLegacy(%q).Plain() = %q, Clean %q", s, got, want)
	}
	if got, want := p.ANSI(), nettext.ANSI(s); got != want {
		t.Fatalf("ParseLegacy(%q).ANSI() = %q, want %q", s, got, want)
	}
}

func TestLegacyRoundTrip(t *testing.T) {
	// The case of the tests of Clean in net/bedrock/text.
	if got := text.ParseLegacy("§cRed §lbold§r §zkept").Plain(); got != "Red bold §zkept" {
		t.Errorf("Plain = %q", got)
	}
	for in, want := range colourfCases {
		checkLegacy(t, in)
		checkLegacy(t, want)
	}
	seed := rand.Uint64()
	t.Logf("seed %d", seed)
	for _, r := range []*rand.Rand{rand.New(rand.NewPCG(1, 2)), rand.New(rand.NewPCG(seed, seed))} {
		for range 50000 {
			var b strings.Builder
			for range 1 + r.IntN(16) {
				b.WriteString(legacyFragments[r.IntN(len(legacyFragments))])
			}
			checkLegacy(t, b.String())
			// The strings Colourf gives today are read back as they are.
			checkLegacy(t, nettext.Colourf("%s", randomMarkup(r, 12)))
		}
	}
}

// TestLegacyMeans checks that what the markup means is what its string
// means: Colourf of markup with no § codes is equivalent to the text
// ParseLegacy reads from the string the markup gives.
func TestLegacyMeans(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	for range 50000 {
		m := randomMarkup(r, 12)
		c := text.Colourf("%s", m)
		if strings.Contains(c.Plain(), "§") {
			continue
		}
		if p := text.ParseLegacy(String(c)); !text.Equivalent(c, p) {
			t.Fatalf("Colourf(%q) = %#v, not equivalent to %#v", m, c, p)
		}
	}
}

// treeReader builds random texts from bytes.
type treeReader struct {
	data []byte
	// bedrock limits texts to what Bedrock Edition shows: named colours,
	// no underlined or struck through text, no translations and no §.
	bedrock bool
}

// next returns the next byte, 0 at the end.
func (r *treeReader) next() byte {
	if len(r.data) == 0 {
		return 0
	}
	b := r.data[0]
	r.data = r.data[1:]
	return b
}

// literals are the literal texts of random texts.
var literals = []string{"", "a", "Hello", " ", "§", "c", "x§", "§c", "日本", "\xff", "%s"}

// text returns a random text of no more than depth levels.
func (r *treeReader) text(depth int) text.Text {
	op := r.next()
	var t text.Text
	switch op % 4 {
	case 0, 1:
		l := literals[int(r.next())%len(literals)]
		if r.bedrock && strings.Contains(l, "§") {
			l = "b"
		}
		t = text.Literal(l)
	case 2:
		if !r.bedrock && depth > 0 {
			var args []text.Text
			for range r.next() % 3 {
				args = append(args, r.text(depth-1))
			}
			t = text.Translate("key", []string{"", "%s|%s", "%2$s-%1$s", "%%%s%%", "100%"}[int(r.next())%5], args...)
			break
		}
		t = text.Literal("t")
	case 3:
		// No content.
	}
	if b := r.next(); b%2 == 0 {
		s := text.Style{Reset: b%8 == 0}
		colours := r.next()
		switch {
		case colours < 100:
		case colours < 230 || r.bedrock:
			s.Colour = text.Black + text.Colour(int(colours)%int(text.Resin))
		default:
			s.Colour = text.RGB(r.next(), r.next(), r.next())
		}
		mask := text.AllDecorations
		if r.bedrock {
			mask = text.Obfuscated | text.Bold | text.Italic
		}
		s.On = text.Decoration(r.next()) & mask
		s.Off = text.Decoration(r.next()) & text.AllDecorations
		t = t.WithStyle(s)
	}
	if depth > 0 {
		var children []text.Text
		for range r.next() % 4 {
			children = append(children, r.text(depth-1))
		}
		t = t.Append(children...)
	}
	return t
}

// checkTree fails t if the text tr is not written as it means, or does not
// show on the console as its string does.
func checkTree(t testing.TB, tr text.Text, bedrock bool) {
	t.Helper()
	s := String(tr)
	if got, want := tr.ANSI(), nettext.ANSI(s); got != want {
		t.Fatalf("%#v.ANSI() = %q, its string %q shows as %q", tr, got, s, want)
	}
	if bedrock {
		if p := text.ParseLegacy(s); !text.Equivalent(tr, p) {
			t.Fatalf("%#v renders as %q, which is %#v", tr, s, p)
		}
	}
	if got := String(text.ParseLegacy(s)); got != s {
		t.Fatalf("the string %q of %#v reads back as %q", s, tr, got)
	}
	msg, params, translated := Message(tr)
	if hasTranslation(tr) != translated || !translated && (msg != s || params != nil) {
		t.Fatalf("Message(%#v) = %q, %q, %v", tr, msg, params, translated)
	}
}

// hasTranslation reports whether t holds a translation outside of the
// arguments of translations.
func hasTranslation(t text.Text) bool {
	if _, _, ok := t.Translation(); ok {
		return true
	}
	for c := range t.Children() {
		if hasTranslation(c) {
			return true
		}
	}
	return false
}

func TestTreesMean(t *testing.T) {
	r := rand.New(rand.NewPCG(5, 6))
	data := make([]byte, 200)
	for range 30000 {
		for i := range data {
			data[i] = byte(r.Uint32())
		}
		bedrock := r.IntN(2) == 0
		checkTree(t, (&treeReader{data: slices.Clone(data), bedrock: bedrock}).text(4), bedrock)
	}
}

func TestCode(t *testing.T) {
	for in, want := range map[text.Code]string{
		{Colour: text.Red}:                                   "§c",
		{Colour: text.Orange}:                                "§6",
		{Colour: text.DarkYellow}:                            "§g",
		{Colour: text.Gold}:                                  "§p",
		{Colour: text.Redstone}:                              "§m",
		{Colour: text.Copper}:                                "§n",
		{Colour: text.Resin}:                                 "§v",
		{Colour: text.RGB(0xFF, 0x50, 0x50)}:                 "§c",
		{Colour: text.RGB(0xEE, 0x22, 0x2C)}:                 "§m",
		{Colour: text.Colour(99)}:                            "",
		{Decoration: text.Obfuscated}:                        "§k",
		{Decoration: text.Bold}:                              "§l",
		{Decoration: text.Italic}:                            "§o",
		{Decoration: text.Underlined}:                        "",
		{Decoration: text.Strikethrough}:                     "",
		{Decoration: text.Bold | text.Italic}:                "",
		{Colour: text.NoColour, Decoration: text.Obfuscated}: "§k",
	} {
		if got := Code(in); got != want {
			t.Errorf("Code(%v) = %q, want %q", in, got, want)
		}
	}
	// Every named colour has the code its tag stands for in Colourf.
	for c := text.Black; c <= text.Resin; c++ {
		tag := c.String()
		if got, want := String(text.Colourf("%s", "<"+tag+">x")), nettext.Colourf("%s", "<"+tag+">x"); got != want {
			t.Errorf("%v renders as %q, want %q", c, got, want)
		}
		if got := String(text.Literal("x").WithStyle(text.Style{Colour: c})); got != Code(text.Code{Colour: c})+"x" {
			t.Errorf("%v renders as %q", c, got)
		}
	}
}

// key is a translation key that resolves to itself, as the game's keys do.
type key string

func (k key) Resolve(language.Tag) string { return string(k) }

// TestMessageMatchesChat checks that a translation built with the model
// renders as the chat package sends translations today: the message and
// parameters of a TextPacket that needs translation, and the fallback for
// subscribers that do not translate.
func TestMessageMatchesChat(t *testing.T) {
	steve, alex := text.Literal("Steve"), text.Literal("§cAlex")
	stone := chat.Translate(key("%tile.stone.name"), 0, "Stone")
	for _, c := range []struct {
		name  string
		old   chat.Translation
		args  []any
		model text.Text
		// reason, if set, is why the fallback of the model differs from
		// that of the chat package.
		reason string
	}{
		{"join", chat.MessageJoin, []any{"Steve"},
			text.Colourf("<yellow>%v</yellow>", text.Translate("multiplayer.player.joined", "%s joined the game", steve)), ""},
		{"no format", chat.MessageSleeping, []any{"Steve", 2},
			text.Colourf("%v", text.Translate("chat.type.sleeping", "%s is sleeping in a bed. To skip to dawn, %s more users need to sleep in beds at the same time.", steve, text.Literal("2"))), ""},
		{"nested tags", chat.MessageAdminCommand, []any{"§cAlex", "Made it day"},
			text.Colourf("<grey><i>%v</i></grey>", text.Translate("chat.type.admin", "[%s: %s]", alex, text.Literal("Made it day"))), ""},
		{"no parameters", chat.MessageBedTooFar, nil,
			text.Colourf("<grey>%v</grey>", text.Translate("tile.bed.tooFar", "Bed is too far away")), ""},
		{"a translation as a parameter", chat.Translate(key("%death.attack.mob"), 2, "%v was slain by %v"), []any{"Steve", stone},
			text.Colourf("%v", text.Translate("death.attack.mob", "%s was slain by %s", steve, text.Colourf("%v", text.Translate("tile.stone.name", "Stone")))), ""},
		{"the key of a mob as a parameter", chat.Translate(key("%death.attack.mob"), 2, "%v was slain by %v"), []any{"Steve", "%entity.zombie.name"},
			text.Colourf("%v", text.Translate("death.attack.mob", "%s was slain by %s", steve, text.Translate("entity.zombie.name", "Zombie"))),
			// The game passes the key of a mob as a string, which the
			// fallback of the chat package shows as it is.
			"the chat package shows the key of the mob in the fallback"},
	} {
		l := language.English
		old := c.old.F(c.args...)
		msg, params, translated := Message(c.model)
		if !translated || msg != old.Resolve(l) || !slices.Equal(params, old.Params(l)) {
			t.Errorf("%s: Message = %q, %q, %v; the chat package sends %q, %q", c.name, msg, params, translated, old.Resolve(l), old.Params(l))
		}
		if got, want := String(c.model), old.String(); got != want && c.reason == "" {
			t.Errorf("%s: String = %q; the chat package falls back to %q", c.name, got, want)
		} else if got == want && c.reason != "" {
			t.Errorf("%s: String is what the chat package falls back to, %q, though %s", c.name, got, c.reason)
		}
	}
}

func TestMessage(t *testing.T) {
	red := text.Style{Colour: text.Red}
	for _, c := range []struct {
		t      text.Text
		msg    string
		params []string
	}{
		{text.Translate("k", "f"), "%k", nil},
		{text.Translate("k", "f %s", text.Literal("x").WithStyle(red)), "%k", []string{"§cx"}},
		// A parameter with a translation that has arguments is filled in.
		{text.Translate("k", "%s", text.Translate("j", "<%s>", text.Literal("x"))), "%k", []string{"<x>"}},
		{text.Translate("k", "%s", text.Concat(text.Literal("a"), text.Translate("j", "J"))), "%k", []string{"a%j"}},
		// Translations one after another, their parameters in order.
		{text.Concat(text.Translate("a", "", text.Literal("1")).WithStyle(red), text.Literal(" "), text.Translate("b", "", text.Literal("2"))), "§c%a§r %b", []string{"1", "2"}},
	} {
		msg, params, translated := Message(c.t)
		if !translated || msg != c.msg || !slices.Equal(params, c.params) {
			t.Errorf("Message(%#v) = %q, %q, %v, want %q, %q", c.t, msg, params, translated, c.msg, c.params)
		}
	}
	if msg, params, translated := Message(text.Text{}); msg != "" || params != nil || translated {
		t.Errorf("Message of the zero Text = %q, %q, %v", msg, params, translated)
	}
}

// chatMessage is a message as the game sends many: a name and a message in
// colours, made with Colourf.
func chatMessage() text.Text {
	return text.Colourf("<grey>[<aqua>%v</aqua>]</grey> <white>%v</white>", "Steve", "Hello, world! How are you all doing today?")
}

func TestAllocations(t *testing.T) {
	m := chatMessage()
	if n := testing.AllocsPerRun(100, func() { _ = String(m) }); n > 3 {
		t.Errorf("String allocates %v times, want no more than 3", n)
	}
	buf := make([]byte, 0, 256)
	if n := testing.AllocsPerRun(100, func() { buf = Append(buf[:0], m) }); n > 2 {
		t.Errorf("Append allocates %v times, want no more than 2", n)
	}
}

func BenchmarkString(b *testing.B) {
	m := chatMessage()
	b.ReportAllocs()
	for b.Loop() {
		_ = String(m)
	}
}

func BenchmarkNetColourf(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = nettext.Colourf("<grey>[<aqua>%v</aqua>]</grey> <white>%v</white>", "Steve", "Hello, world! How are you all doing today?")
	}
}

func BenchmarkColourfString(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = String(chatMessage())
	}
}
