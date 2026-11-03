package javatext

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/df-mc/dragonfly/server/text"
)

func TestJSON(t *testing.T) {
	red := text.Style{Colour: text.Red}
	for _, c := range []struct {
		name string
		t    text.Text
		want string
	}{
		{"zero", text.Text{}, `""`},
		{"literal", text.Literal("a"), `"a"`},
		{"empty literal", text.Literal(""), `""`},
		{"styled", text.Literal("a").WithStyle(red), `{"text":"a","color":"red"}`},
		{"no content", text.Concat(text.Literal("a"), text.Literal("b")), `{"text":"","extra":["a","b"]}`},
		{"decorations", text.Literal("a").WithStyle(text.Style{On: text.AllDecorations}), `{"text":"a","obfuscated":true,"bold":true,"strikethrough":true,"underlined":true,"italic":true}`},
		{"decorations off", text.Literal("a").WithStyle(text.Style{On: text.Bold, Off: text.Italic | text.Underlined}), `{"text":"a","bold":true,"underlined":false,"italic":false}`},
		{"material", text.Literal("a").WithStyle(text.Style{Colour: text.Redstone}), `{"text":"a","color":"#EE222C"}`},
		{"rgb", text.Literal("a").WithStyle(text.Style{Colour: text.RGB(1, 0xAB, 0xFF)}), `{"text":"a","color":"#01ABFF"}`},
		{"translation", text.Translate("multiplayer.player.joined", "%s joined the game", text.Literal("Steve").WithStyle(red)),
			`{"translate":"multiplayer.player.joined","fallback":"%s joined the game","with":[{"text":"Steve","color":"red"}]}`},
		{"translation without fallback", text.Translate("k", "").WithStyle(text.Style{On: text.Bold}).Append(text.Literal("x")), `{"translate":"k","bold":true,"extra":["x"]}`},
		{"translation with a zero argument", text.Translate("k", "", text.Text{}), `{"translate":"k","with":[""]}`},
		{"children", text.Literal("a").WithStyle(red).Append(text.Literal("b").WithStyle(text.Style{On: text.Bold})),
			`{"text":"a","color":"red","extra":[{"text":"b","bold":true}]}`},
		{"reset within", text.Literal("a").WithStyle(text.Style{Colour: text.Red, On: text.Bold | text.Italic}).Append(text.Literal("b").WithStyle(text.Style{Reset: true, On: text.Italic})),
			`{"text":"a","color":"red","bold":true,"italic":true,"extra":[{"text":"b","color":"white","bold":false,"italic":true}]}`},
		{"reset with nothing inherited", text.Concat(text.Literal("a"), text.Literal("b").WithStyle(text.Style{Reset: true})), `{"text":"","extra":["a",{"text":"b"}]}`},
		{"reset of the outermost text", text.Literal("a").WithStyle(text.Style{Reset: true, Colour: text.Blue}), `{"text":"a","color":"blue"}`},
		{"markup", text.Colourf("<red>Hello <bold>World</bold>!</red>"),
			`{"text":"","extra":[{"text":"Hello ","color":"red"},{"text":"","color":"red","extra":[{"text":"World","bold":true}]},{"text":"!","color":"red"},""]}`},
		{"legacy", text.ParseLegacy("§cA§lB§r§9C"), `{"text":"","extra":[{"text":"A","color":"red","extra":[{"text":"B","bold":true}]},{"text":"C","color":"blue"}]}`},
		{"escapes", text.Literal("\"\\\n\r\t\x01\x1f\x7f/<>&\xff日本"), `"\"\\\n\r\t\u0001\u001f` + "\x7f/<>&\uFFFD日本\""},
	} {
		got := JSON(c.t)
		if got != c.want {
			t.Errorf("%s: JSON(%#v) = %s, want %s", c.name, c.t, got, c.want)
		}
		if !json.Valid([]byte(got)) {
			t.Errorf("%s: %s is not JSON", c.name, got)
		}
		if a := string(AppendJSON([]byte("x"), c.t)); a != "x"+got {
			t.Errorf("%s: AppendJSON = %s", c.name, a)
		}
	}
}

func TestColourNames(t *testing.T) {
	want := []string{
		"black", "dark_blue", "dark_green", "dark_aqua", "dark_red", "dark_purple", "gold", "gray", "dark_gray",
		"blue", "green", "aqua", "red", "light_purple", "yellow", "white",
	}
	for i, name := range want {
		c := text.Black + text.Colour(i)
		if got := colourName(c); got != name {
			t.Errorf("colourName(%v) = %q, want %q", c, got, name)
		}
	}
	for c := text.DarkYellow; c <= text.Resin; c++ {
		r, g, b := c.RGB()
		if got, want := colourName(c), fmt.Sprintf("#%02X%02X%02X", r, g, b); got != want {
			t.Errorf("colourName(%v) = %q, want %q", c, got, want)
		}
	}
}

// look is how a piece of text looks in the chat: its colour as RGB, no
// colour being white, and its decorations; or a translation.
type look struct {
	rgb         uint32
	decorations text.Decoration
	text        string
	key         string
	fallback    string
	args        [][]look
}

// javaPalette are the RGB values of the colours Java Edition names.
var javaPalette = map[string]uint32{
	"black": 0x000000, "dark_blue": 0x0000AA, "dark_green": 0x00AA00, "dark_aqua": 0x00AAAA, "dark_red": 0xAA0000,
	"dark_purple": 0xAA00AA, "gold": 0xFFAA00, "gray": 0xAAAAAA, "dark_gray": 0x555555, "blue": 0x5555FF,
	"green": 0x55FF55, "aqua": 0x55FFFF, "red": 0xFF5555, "light_purple": 0xFF55FF, "yellow": 0xFFFF55, "white": 0xFFFFFF,
}

// rgbOf returns the RGB value of c, white for NoColour.
func rgbOf(c text.Colour) uint32 {
	if c == text.NoColour {
		return 0xFFFFFF
	}
	r, g, b := c.RGB()
	return uint32(r)<<16 | uint32(g)<<8 | uint32(b)
}

// addLook adds a piece to looks, joining text of the same look.
func addLook(looks []look, l look) []look {
	if l.key == "" && l.text == "" {
		return looks
	}
	if n := len(looks) - 1; n >= 0 && l.key == "" && looks[n].key == "" && looks[n].rgb == l.rgb && looks[n].decorations == l.decorations {
		looks[n].text += l.text
		return looks
	}
	return append(looks, l)
}

// modelLooks returns how t looks within a text of the style in effect s.
func modelLooks(looks []look, t text.Text, s text.Style) []look {
	if t.IsZero() {
		return looks
	}
	s = t.Style().Inherit(s)
	if l, ok := t.Literal(); ok {
		looks = addLook(looks, look{rgb: rgbOf(s.Colour), decorations: s.On, text: l})
	}
	if key, fallback, ok := t.Translation(); ok {
		l := look{rgb: rgbOf(s.Colour), decorations: s.On, key: key, fallback: fallback}
		for a := range t.Args() {
			l.args = append(l.args, modelLooks(nil, a, s))
		}
		looks = append(looks, l)
	}
	for c := range t.Children() {
		looks = modelLooks(looks, c, s)
	}
	return looks
}

// jsonStyle is the style in effect for a component.
type jsonStyle struct {
	rgb         uint32
	decorations text.Decoration
}

// jsonLooks returns how the component v, decoded from JSON, looks within a
// component of the style s, as Java Edition inherits styles.
func jsonLooks(t *testing.T, looks []look, v any, s jsonStyle) []look {
	switch v := v.(type) {
	case string:
		return addLook(looks, look{rgb: s.rgb, decorations: s.decorations, text: v})
	case map[string]any:
		if c, ok := v["color"].(string); ok {
			if rgb, ok := javaPalette[c]; ok {
				s.rgb = rgb
			} else if n, err := strconv.ParseUint(strings.TrimPrefix(c, "#"), 16, 32); err == nil && strings.HasPrefix(c, "#") {
				s.rgb = uint32(n)
			} else {
				t.Fatalf("colour %q", c)
			}
		}
		for _, d := range decorations {
			if on, ok := v[d.key].(bool); ok {
				if on {
					s.decorations |= d.d
				} else {
					s.decorations &^= d.d
				}
			}
		}
		if key, ok := v["translate"].(string); ok {
			l := look{rgb: s.rgb, decorations: s.decorations, key: key}
			l.fallback, _ = v["fallback"].(string)
			with, _ := v["with"].([]any)
			for _, a := range with {
				l.args = append(l.args, jsonLooks(t, nil, a, s))
			}
			looks = append(looks, l)
		} else {
			lit, ok := v["text"].(string)
			if !ok {
				t.Fatalf("component without text: %v", v)
			}
			looks = addLook(looks, look{rgb: s.rgb, decorations: s.decorations, text: lit})
		}
		extra, _ := v["extra"].([]any)
		for _, e := range extra {
			looks = jsonLooks(t, looks, e, s)
		}
		return looks
	}
	t.Fatalf("component %#v", v)
	return nil
}

// randomText returns a random text of no more than depth levels.
func randomText(r *rand.Rand, depth int) text.Text {
	var t text.Text
	switch r.IntN(4) {
	case 0, 1:
		t = text.Literal([]string{"", "a", "Hello", " ", "§c", "\"\\", "日本", "\xff"}[r.IntN(8)])
	case 2:
		var args []text.Text
		for range r.IntN(3) {
			if depth > 0 {
				args = append(args, randomText(r, depth-1))
			}
		}
		t = text.Translate([]string{"k", "chat.type.text"}[r.IntN(2)], []string{"", "%s", "%s and %s"}[r.IntN(3)], args...)
	}
	if r.IntN(2) == 0 {
		s := text.Style{Reset: r.IntN(5) == 0, On: text.Decoration(r.UintN(32)), Off: text.Decoration(r.UintN(32))}
		switch r.IntN(3) {
		case 1:
			s.Colour = text.Black + text.Colour(r.IntN(int(text.Resin)))
		case 2:
			s.Colour = text.RGB(uint8(r.UintN(256)), uint8(r.UintN(256)), uint8(r.UintN(256)))
		}
		t = t.WithStyle(s)
	}
	if depth > 0 {
		for range r.IntN(4) {
			t = t.Append(randomText(r, depth-1))
		}
	}
	return t
}

// TestLooksTheSame checks that random texts look in their JSON, read as
// Java Edition inherits styles, as they look in the model, where they are
// shown in white with no decorations.
func TestLooksTheSame(t *testing.T) {
	r := rand.New(rand.NewPCG(7, 8))
	for range 20000 {
		tr := randomText(r, 4)
		j := JSON(tr)
		var v any
		if err := json.Unmarshal([]byte(j), &v); err != nil {
			t.Fatalf("JSON(%#v) = %s: %v", tr, j, err)
		}
		// The outermost Reset is not written, and has no effect where text
		// has no style.
		want := modelLooks(nil, tr, text.Style{})
		got := jsonLooks(t, nil, v, jsonStyle{rgb: 0xFFFFFF})
		if !slices.EqualFunc(got, want, equalLook) {
			t.Fatalf("JSON(%#v) = %s\nlooks %+v\nwant %+v", tr, j, got, want)
		}
	}
}

// equalLook reports whether a and b look the same, with text that is not
// UTF-8 as JSON reads it.
func equalLook(a, b look) bool {
	if a.rgb != b.rgb || a.decorations != b.decorations || a.key != b.key || a.fallback != b.fallback || len(a.args) != len(b.args) {
		return false
	}
	if validUTF8(a.text) != validUTF8(b.text) {
		return false
	}
	for i := range a.args {
		if !slices.EqualFunc(a.args[i], b.args[i], equalLook) {
			return false
		}
	}
	return true
}

// validUTF8 returns s with each byte that is not UTF-8 replaced by U+FFFD,
// as JSON writes it.
func validUTF8(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			b.WriteString("\uFFFD")
		} else {
			b.WriteString(s[i : i+size])
		}
		i += size
	}
	return b.String()
}

func BenchmarkJSON(b *testing.B) {
	m := text.Colourf("<grey>[<aqua>%v</aqua>]</grey> <white>%v</white>", "Steve", "Hello, world!")
	buf := make([]byte, 0, 512)
	b.ReportAllocs()
	for b.Loop() {
		buf = AppendJSON(buf[:0], m)
	}
}
