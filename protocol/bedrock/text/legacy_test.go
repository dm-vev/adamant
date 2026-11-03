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

	"github.com/df-mc/dragonfly/server/text"
	nettext "github.com/dm-vev/adamant/net/bedrock/text"
)

// FuzzLegacyWriter compares the legacy writer of server/text, Text.Legacy,
// which String and Append are, with the renderer this package had before
// it (oldString), on every kind of text the other fuzz tests make: the random trees of FuzzTree, in both modes, the
// texts Colourf makes of the markup of FuzzColourf, and those ParseLegacy
// reads from the strings of FuzzLegacy. Their corpora are its seeds.
func FuzzLegacyWriter(f *testing.F) {
	for _, in := range corpus(f, "FuzzTree") {
		f.Add(in[0].([]byte), in[1].(bool), "", "", int64(0))
	}
	f.Add([]byte{}, false, "", "", int64(0))
	f.Add([]byte{0, 0, 0, 1, 0, 200, 2, 0, 3}, true, "", "", int64(0))
	f.Add([]byte{3, 0, 120, 6, 2, 3, 0, 1, 0, 8, 150, 2, 1, 2, 0, 2, 0, 2, 1}, true, "", "", int64(0))
	f.Add([]byte{2, 2, 1, 0, 3, 0, 240, 1, 2, 3, 4, 1}, false, "", "", int64(0))
	f.Add([]byte("random bytes of a text, long enough for some nesting and styles to come out of them"), false, "", "", int64(0))
	for _, in := range corpus(f, "FuzzColourf") {
		f.Add([]byte{}, false, in[0].(string), in[1].(string), in[2].(int64))
	}
	for in, want := range colourfCases {
		f.Add([]byte{}, false, in, want, int64(0))
	}
	for _, s := range fragments {
		f.Add([]byte{}, false, "<red>"+s+"%v</red>", s, int64(2))
	}
	for _, in := range corpus(f, "FuzzLegacy") {
		f.Add([]byte{}, true, in[0].(string), "", int64(0))
	}
	for _, s := range legacyFragments {
		f.Add([]byte{}, true, "a"+s+"§c"+s+"§r", s, int64(0))
	}
	f.Fuzz(func(t *testing.T, data []byte, bedrock bool, format, arg string, n int64) {
		checkLegacyWriter(t, (&treeReader{data: data, bedrock: bedrock}).text(5))
		checkLegacyWriter(t, colourf(format, nil))
		checkLegacyWriter(t, colourf("%s", []any{format}))
		checkLegacyWriter(t, colourf(format, []any{arg, n}))
		checkLegacyWriter(t, colourf(format, []any{text.ParseLegacy(arg), text.Literal(arg)}))
		checkLegacyWriter(t, text.ParseLegacy(format))
		checkLegacyWriter(t, text.ParseLegacy(arg))
	})
}

// colourf calls text.Colourf. It takes the arguments as a slice, so that
// vet does not hold a format that is not a constant against it.
func colourf(format string, a []any) text.Text {
	return text.Colourf(format, a...)
}

// checkLegacyWriter fails t if the legacy writer of server/text, through
// String and Append, does not write tr as the renderer this package had
// before it did, or writes one of its codes not as that renderer did.
func checkLegacyWriter(t testing.TB, tr text.Text) {
	t.Helper()
	want := oldAppend([]byte("x"), tr)
	if got := String(tr); got != string(want[1:]) {
		t.Fatalf("String(%#v) = %q, the old renderer wrote %q", tr, got, want[1:])
	}
	if got := Append([]byte("x"), tr); !slices.Equal(got, want) {
		t.Fatalf("Append of %#v = %q, the old renderer wrote %q", tr, got, want)
	}
	for s := range tr.Spans(text.SpanOptions{Decorations: text.AllDecorations}) {
		for _, c := range s.Codes {
			checkCodeLegacy(t, c)
		}
	}
}

// checkCodeLegacy fails t if Code(c) is not the code the old renderer
// wrote for c.
func checkCodeLegacy(t testing.TB, c text.Code) {
	t.Helper()
	if got, want := Code(c), oldCode(c); got != want {
		t.Fatalf("Code(%v) = %q, the old renderer wrote %q", c, got, want)
	}
}

// The renderer of this package before it wrote text with the legacy
// writer of server/text, kept as it was so that FuzzLegacyWriter keeps
// comparing the two.

// oldCodes are the § codes of the named colours.
var oldCodes = [...]string{
	text.Black:      nettext.Black,
	text.DarkBlue:   nettext.DarkBlue,
	text.DarkGreen:  nettext.DarkGreen,
	text.DarkAqua:   nettext.DarkAqua,
	text.DarkRed:    nettext.DarkRed,
	text.DarkPurple: nettext.DarkPurple,
	text.Orange:     nettext.Orange,
	text.Grey:       nettext.Grey,
	text.DarkGrey:   nettext.DarkGrey,
	text.Blue:       nettext.Blue,
	text.Green:      nettext.Green,
	text.Aqua:       nettext.Aqua,
	text.Red:        nettext.Red,
	text.Purple:     nettext.Purple,
	text.Yellow:     nettext.Yellow,
	text.White:      nettext.White,
	text.DarkYellow: nettext.DarkYellow,
	text.Quartz:     nettext.Quartz,
	text.Iron:       nettext.Iron,
	text.Netherite:  nettext.Netherite,
	text.Redstone:   nettext.Redstone,
	text.Copper:     nettext.Copper,
	text.Gold:       nettext.Gold,
	text.Emerald:    nettext.Emerald,
	text.Diamond:    nettext.Diamond,
	text.Lapis:      nettext.Lapis,
	text.Amethyst:   nettext.Amethyst,
	text.Resin:      nettext.Resin,
}

// oldCode returns the § code of c.
func oldCode(c text.Code) string {
	if c.Colour != text.NoColour {
		if n := c.Colour.Nearest(); n != text.NoColour {
			return oldCodes[n]
		}
		return ""
	}
	switch c.Decoration {
	case text.Obfuscated:
		return nettext.Obfuscated
	case text.Bold:
		return nettext.Bold
	case text.Italic:
		return nettext.Italic
	}
	return ""
}

// oldAppend appends t as a string with § codes to dst and returns the
// result.
func oldAppend(dst []byte, t text.Text) []byte {
	for s := range t.Spans(text.SpanOptions{Decorations: text.Obfuscated | text.Bold | text.Italic}) {
		if s.Reset {
			dst = append(dst, nettext.Reset...)
		}
		for _, c := range s.Codes {
			dst = append(dst, oldCode(c)...)
		}
		dst = append(dst, s.Text...)
	}
	return dst
}

func TestLegacyWriterCodes(t *testing.T) {
	colours := []text.Colour{text.NoColour, text.Resin + 1, text.Colour(99), 1<<24 - 1, 1<<25 | 5}
	for c := text.Black; c <= text.Resin; c++ {
		colours = append(colours, c)
	}
	r := rand.New(rand.NewPCG(9, 10))
	for range 20000 {
		colours = append(colours, text.RGB(uint8(r.Uint32()), uint8(r.Uint32()), uint8(r.Uint32())))
	}
	for _, c := range colours {
		for d := range 256 {
			checkCodeLegacy(t, text.Code{Colour: c, Decoration: text.Decoration(d)})
		}
	}
}

func TestLegacyWriter(t *testing.T) {
	r := rand.New(rand.NewPCG(11, 12))
	data := make([]byte, 200)
	for range 30000 {
		for i := range data {
			data[i] = byte(r.Uint32())
		}
		checkLegacyWriter(t, (&treeReader{data: slices.Clone(data), bedrock: r.IntN(2) == 0}).text(4))
		m := randomMarkup(r, 12)
		checkLegacyWriter(t, colourf("%s", []any{m}))
		checkLegacyWriter(t, colourf(m, []any{randomMarkup(r, 3), r.IntN(100)}))
		var b strings.Builder
		for range 1 + r.IntN(16) {
			b.WriteString(legacyFragments[r.IntN(len(legacyFragments))])
		}
		checkLegacyWriter(t, text.ParseLegacy(b.String()))
	}
}

// corpus returns the inputs of the seed corpus of the fuzz test of the name
// passed in testdata/fuzz, each as the values of its lines.
func corpus(t testing.TB, name string) [][]any {
	dir := filepath.Join("testdata", "fuzz", name)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read the corpus of %s: %v", name, err)
	}
	var inputs [][]any
	for _, e := range entries {
		f, err := os.Open(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		var in []any
		sc := bufio.NewScanner(f)
		sc.Buffer(nil, 1<<20)
		for sc.Scan() {
			line := sc.Text()
			if line == "go test fuzz v1" || line == "" {
				continue
			}
			v, err := corpusValue(line)
			if err != nil {
				t.Fatalf("%s/%s: %v", name, e.Name(), err)
			}
			in = append(in, v)
		}
		_ = f.Close()
		inputs = append(inputs, in)
	}
	if len(inputs) == 0 {
		t.Fatalf("the corpus of %s is empty", name)
	}
	return inputs
}

// corpusValue reads a value of a line of a corpus file, of the types the
// fuzz tests of this package take.
func corpusValue(line string) (any, error) {
	typ, v, ok := strings.Cut(line, "(")
	if !ok || !strings.HasSuffix(v, ")") {
		return nil, strconv.ErrSyntax
	}
	v = strings.TrimSuffix(v, ")")
	switch typ {
	case "string":
		return strconv.Unquote(v)
	case "[]byte":
		s, err := strconv.Unquote(v)
		return []byte(s), err
	case "bool":
		return strconv.ParseBool(v)
	case "int64":
		return strconv.ParseInt(v, 0, 64)
	}
	return nil, strconv.ErrSyntax
}
