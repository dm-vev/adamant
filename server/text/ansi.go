package text

import "strings"

// ANSI codes of the colours and decorations a console shows, as the
// console has always shown the § codes of the game. Obfuscated text shows
// as plain text.
const (
	ansiReset      = "\x1b[m"
	ansiObfuscated = ""
	ansiBold       = "\x1b[1m"
	ansiItalic     = "\x1b[3m"
)

// ansiColours are the ANSI codes of the named colours.
var ansiColours = [...]string{
	Black:      "\x1b[38;5;16m",
	DarkBlue:   "\x1b[38;5;19m",
	DarkGreen:  "\x1b[38;5;34m",
	DarkAqua:   "\x1b[38;5;37m",
	DarkRed:    "\x1b[38;5;124m",
	DarkPurple: "\x1b[38;5;127m",
	Orange:     "\x1b[38;5;214m",
	Grey:       "\x1b[38;5;145m",
	DarkGrey:   "\x1b[38;5;59m",
	Blue:       "\x1b[38;5;63m",
	Green:      "\x1b[38;5;83m",
	Aqua:       "\x1b[38;5;87m",
	Red:        "\x1b[38;5;203m",
	Purple:     "\x1b[38;5;207m",
	Yellow:     "\x1b[38;5;227m",
	White:      "\x1b[38;5;231m",
	DarkYellow: "\x1b[38;5;226m",
	Quartz:     "\x1b[38;5;224m",
	Iron:       "\x1b[38;5;251m",
	Netherite:  "\x1b[38;5;234m",
	Redstone:   "\x1b[38;5;1m",
	Copper:     "\x1b[38;5;216m",
	Gold:       "\x1b[38;5;220m",
	Emerald:    "\x1b[38;5;71m",
	Diamond:    "\x1b[38;5;122m",
	Lapis:      "\x1b[38;5;4m",
	Amethyst:   "\x1b[38;5;171m",
	Resin:      "\x1b[38;5;172m",
}

// ansiDecorations are the decorations a console shows: those of Bedrock
// Edition, whose codes the console has always turned into ANSI codes.
const ansiDecorations = Obfuscated | Bold | Italic

// ansiCode returns the ANSI code of c.
func ansiCode(c Code) string {
	if c.Colour != NoColour {
		return ansiColours[c.Colour.Nearest()]
	}
	switch c.Decoration {
	case Obfuscated:
		return ansiObfuscated
	case Bold:
		return ansiBold
	case Italic:
		return ansiItalic
	}
	return ""
}

// legacyANSI turns § codes in literal text into ANSI codes, as the console
// has always turned those of the strings it prints.
var legacyANSI = func() *strings.Replacer {
	var pairs []string
	for c := 0; c < len(legacyCodes); c++ {
		code := legacyCodes[c]
		if !code.ok {
			continue
		}
		ansi := ansiReset
		if !code.reset {
			ansi = ansiCode(code.code)
		}
		pairs = append(pairs, "§"+string(rune(c)), ansi)
	}
	return strings.NewReplacer(pairs...)
}()

// ANSI returns t with ANSI codes for a terminal, as the console of the
// game prints text: t is written as a stream of Bedrock Edition's codes
// would be (see Spans), translations filled in, but with ANSI codes in
// place of § codes, a colour of RGB as the named colour nearest to it. §
// codes in literal text are turned into ANSI codes too, as the console does
// with the § codes of any string it prints. No reset is added at the end.
//
// For a text that ParseLegacy reads from a string, or Colourf makes, it is
// exactly what the console prints for that string or for the string the
// markup gives.
func (t Text) ANSI() string {
	var b, run strings.Builder
	flush := func() {
		if run.Len() > 0 {
			legacyANSI.WriteString(&b, run.String())
			run.Reset()
		}
	}
	for s := range t.Spans(SpanOptions{Decorations: ansiDecorations}) {
		if s.Reset || len(s.Codes) > 0 {
			// The § code a code stands for and the text around it never
			// make a code together: text ending in § or its first byte
			// would be followed by the first byte of §, which is neither
			// the second byte of § nor the character of a code. So the
			// runs of text between codes are turned on their own, and
			// the result is what the console prints for the string of §
			// codes.
			flush()
			if s.Reset {
				b.WriteString(ansiReset)
			}
			for _, c := range s.Codes {
				b.WriteString(ansiCode(c))
			}
		}
		run.WriteString(s.Text)
	}
	flush()
	return b.String()
}
