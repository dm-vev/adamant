package text

import (
	"strconv"
	"strings"
)

// Colour is the colour of text: NoColour, which keeps the colour a text
// inherits, one of the named colours below, or a colour of any RGB value
// made with RGB. The zero Colour is NoColour.
type Colour uint32

// The named colours: the sixteen colours both editions have, then the
// colours of materials Bedrock Edition has besides them. They are named as
// the tags of Colourf and the codes of the net/bedrock/text package of the
// game name them, so Orange is the colour Java Edition calls gold and Gold
// the colour of the material.
const (
	// NoColour keeps the colour a text inherits.
	NoColour Colour = iota
	Black
	DarkBlue
	DarkGreen
	DarkAqua
	DarkRed
	DarkPurple
	Orange
	Grey
	DarkGrey
	Blue
	Green
	Aqua
	Red
	Purple
	Yellow
	White
	// DarkYellow is the colour of Minecoins (minecoin_gold).
	DarkYellow
	Quartz
	Iron
	Netherite
	Redstone
	Copper
	Gold
	Emerald
	Diamond
	Lapis
	Amethyst
	Resin
)

// lastNamed is the last of the named colours, which run from Black.
const lastNamed = Resin

// rgbColour marks a Colour made with RGB; the RGB value takes the low 24
// bits.
const rgbColour Colour = 1 << 24

// colours holds the name and the RGB value of each named colour.
//
// The sixteen colours both editions have take the values Java Edition
// gives them, the classic palette. The material colours exist in Bedrock
// Edition alone; their values are those the Minecraft Wiki lists for
// Bedrock Edition 26.50, which changed them
// (https://minecraft.wiki/w/Formatting_codes, read on 2026-11-03). The
// repository holds no data of its own on them. The wiki gives the decimal
// value of the blue of lapis as 155 but its hex as #577BFF; the hex is
// taken.
var colours = [...]struct {
	name string
	rgb  uint32
}{
	Black:      {"black", 0x000000},
	DarkBlue:   {"dark-blue", 0x0000AA},
	DarkGreen:  {"dark-green", 0x00AA00},
	DarkAqua:   {"dark-aqua", 0x00AAAA},
	DarkRed:    {"dark-red", 0xAA0000},
	DarkPurple: {"dark-purple", 0xAA00AA},
	Orange:     {"orange", 0xFFAA00},
	Grey:       {"grey", 0xAAAAAA},
	DarkGrey:   {"dark-grey", 0x555555},
	Blue:       {"blue", 0x5555FF},
	Green:      {"green", 0x55FF55},
	Aqua:       {"aqua", 0x55FFFF},
	Red:        {"red", 0xFF5555},
	Purple:     {"purple", 0xFF55FF},
	Yellow:     {"yellow", 0xFFFF55},
	White:      {"white", 0xFFFFFF},
	DarkYellow: {"dark-yellow", 0xEFCE16},
	Quartz:     {"quartz", 0xD9CCB8},
	Iron:       {"iron", 0xA9B4B7},
	Netherite:  {"netherite", 0x8F727D},
	Redstone:   {"redstone", 0xEE222C},
	Copper:     {"copper", 0xC87363},
	Gold:       {"gold", 0xFFBF1E},
	Emerald:    {"emerald", 0x13A045},
	Diamond:    {"diamond", 0x5FECFF},
	Lapis:      {"lapis", 0x577BFF},
	Amethyst:   {"amethyst", 0xB66CDD},
	Resin:      {"resin", 0xFF6A00},
}

// RGB returns the colour of the red, green and blue values passed. It is
// never a named colour, even if its value is that of one: renderers show it
// as the value it has where they can.
func RGB(r, g, b uint8) Colour {
	return rgbColour | Colour(r)<<16 | Colour(g)<<8 | Colour(b)
}

// Named reports whether c is one of the named colours.
func (c Colour) Named() bool { return c >= Black && c <= lastNamed }

// IsRGB reports whether c is a colour made with RGB.
func (c Colour) IsRGB() bool { return c&^0xFFFFFF == rgbColour }

// valid reports whether c is NoColour, a named colour or an RGB colour.
func (c Colour) valid() bool { return c == NoColour || c.Named() || c.IsRGB() }

// RGB returns the red, green and blue values of c: those of the colour for
// a named colour, zero for NoColour.
func (c Colour) RGB() (r, g, b uint8) {
	var v uint32
	switch {
	case c.Named():
		v = colours[c].rgb
	case c.IsRGB():
		v = uint32(c & 0xFFFFFF)
	}
	return uint8(v >> 16), uint8(v >> 8), uint8(v)
}

// Nearest returns the named colour closest to c: c itself if it is named,
// NoColour for NoColour. Closest is by the distance of the RGB values,
// among all the named colours; a tie goes to the colour named first.
func (c Colour) Nearest() Colour {
	if !c.IsRGB() {
		if c.Named() {
			return c
		}
		return NoColour
	}
	r, g, b := c.RGB()
	best, bestDist := Black, -1
	for n := Black; n <= lastNamed; n++ {
		nr, ng, nb := n.RGB()
		dr, dg, db := int(r)-int(nr), int(g)-int(ng), int(b)-int(nb)
		if d := dr*dr + dg*dg + db*db; bestDist < 0 || d < bestDist {
			best, bestDist = n, d
		}
	}
	return best
}

// String returns the name of c as the tags of Colourf write it, such as
// "dark-blue", "#rrggbb" for an RGB colour, and "none" for NoColour or a
// value that is no colour.
func (c Colour) String() string {
	switch {
	case c.Named():
		return colours[c].name
	case c.IsRGB():
		s := strconv.FormatUint(uint64(c&0xFFFFFF), 16)
		return "#" + strings.Repeat("0", 6-len(s)) + s
	}
	return "none"
}

// Decoration is a set of the decorations text can have besides its colour.
// The decorations are ordered as the codes of Java Edition's legacy format
// order them (k, l, m, n and o), and renderers that write codes write them
// in this order.
type Decoration uint8

// The decorations of text. Bedrock Edition shows obfuscated, bold and
// italic text; it has no underlined or struck through text.
const (
	Obfuscated Decoration = 1 << iota
	Bold
	Strikethrough
	Underlined
	Italic

	// AllDecorations is the set of all decorations.
	AllDecorations = Obfuscated | Bold | Strikethrough | Underlined | Italic
)

// decorationNames are the names of the decorations, in their order.
var decorationNames = [...]string{"obfuscated", "bold", "strikethrough", "underlined", "italic"}

// String returns the names of the decorations of d, joined with "|", or
// "none" for none.
func (d Decoration) String() string {
	if d&AllDecorations == 0 {
		return "none"
	}
	var b strings.Builder
	for i, name := range decorationNames {
		if d&(1<<i) != 0 {
			if b.Len() > 0 {
				b.WriteByte('|')
			}
			b.WriteString(name)
		}
	}
	return b.String()
}

// Style is the style of a text: its colour and decorations. A text
// inherits the style of the text it is a child of, as components of Java
// Edition do, and its style overrides what it inherits: a colour other
// than NoColour replaces the colour inherited, decorations in On are turned
// on and those in Off turned off. A text with Reset inherits nothing, not
// even the style of where it is shown; it is what §r does in a legacy
// string. The zero Style changes nothing. Style values are comparable.
type Style struct {
	// Colour is the colour of the text, NoColour to keep the colour
	// inherited.
	Colour Colour
	// On are the decorations turned on, Off those turned off. A decoration
	// in both is on.
	On, Off Decoration
	// Reset makes the text inherit no style.
	Reset bool
}

// normalised returns s without what has no effect: Off loses the
// decorations of On, and all of them with Reset, since nothing is
// inherited that they could turn off; a value that is no colour becomes
// NoColour.
func (s Style) normalised() Style {
	s.On &= AllDecorations
	s.Off &= AllDecorations &^ s.On
	if s.Reset {
		s.Off = 0
	}
	if !s.Colour.valid() {
		s.Colour = NoColour
	}
	return s
}

// Inherit returns the style in effect for a text of style s within a text
// for which the style parent is in effect: the colour and the decorations
// on, as Off and Reset are not set in a style in effect. The style in
// effect for a text that is within no other is its style inheriting the
// zero Style.
func (s Style) Inherit(parent Style) Style {
	if s.Reset {
		parent = Style{}
	}
	if s.Colour != NoColour {
		parent.Colour = s.Colour
	}
	parent.On = (parent.On | s.On) &^ s.Off
	return parent
}
