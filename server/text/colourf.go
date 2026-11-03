package text

import (
	"fmt"
	"html"
	"io"
	"strconv"
	"strings"
)

// Colourf formats the format string with the arguments passed, as
// fmt.Sprintf does, and reads the result as markup: tags of colours and
// decorations, which may be nested, around text, such as
// `<red>Hello <bold>World</bold>!</red>`. The tags are the colours black,
// dark-blue, dark-green, dark-aqua, dark-red, dark-purple, orange, grey,
// dark-grey, blue, green, aqua, red, purple, yellow, white, dark-yellow,
// quartz, iron, netherite, redstone, copper, gold, emerald, diamond, lapis,
// amethyst and resin, and obfuscated, bold (b) and italic (i), in any case.
// The rest is read as HTML is: other tags are kept as text, without their
// attributes, comments, doctypes and self-closing tags dropped, the content
// of raw elements such as <script> kept as text, character references
// unescaped and line breaks turned into "\n". The arguments are formatted
// before the markup is read, so markup in them is read too.
//
// This is the markup of Colourf in the net/bedrock/text package of the
// game, and the text returned, written as Spans writes text, gives the
// string of § codes that Colourf gives. The text is built so: it has
// Reset, for the §r that string starts with, unless the formatted string
// is empty; each piece of text is a child of it, nested in a text for each
// tag open around it, the outermost first, so that a stream of codes
// resets after the piece as the string does; and if the last piece is
// within tags, an empty text ends it, for the reset that ends the string.
// A closing tag closes the outermost of the open tags of its name, as it
// always has. § codes in the text are kept as text.
//
// An argument that is a Text and is formatted with %v or %s, with no flags,
// width or precision, is not formatted but placed as it is where it is
// formatted, within the tags around it; with other verbs, its plain text is
// formatted. A Text placed in a tag or a comment is dropped with it.
func Colourf(format string, a ...any) Text {
	texts := 0
	for _, v := range a {
		if _, ok := v.(Text); ok {
			texts++
		}
	}
	if texts == 0 {
		return markup(fmt.Sprintf(format, a...), nil)
	}
	args := make([]any, len(a))
	var embedded []Text
	written := 0
	for i, v := range a {
		if t, ok := v.(Text); ok {
			args[i] = textArg{t: t, index: len(embedded), written: &written}
			embedded = append(embedded, t)
			continue
		}
		args[i] = v
	}
	s := fmt.Sprintf(format, args...)
	if strings.Count(s, argStart) != written {
		// The format or an argument holds the marks of the arguments
		// already: format the texts as their plain text instead.
		for i, v := range a {
			if t, ok := v.(Text); ok {
				args[i] = t.Plain()
			}
		}
		return markup(fmt.Sprintf(format, args...), nil)
	}
	return markup(s, embedded)
}

// argStart and argEnd mark where a Text passed to Colourf goes, around its
// index in decimal. They are noncharacters, which Unicode keeps for such
// internal use; a format or argument that holds them anyway makes Colourf
// format its texts as their plain text.
const (
	argStart = "\uFDD0"
	argEnd   = "\uFDD1"
)

// textArg is a Text passed to Colourf, which it formats as a mark.
type textArg struct {
	t       Text
	index   int
	written *int
}

// Format writes the mark of the argument for %v and %s with no flags,
// width or precision, and the plain text of the argument otherwise.
func (a textArg) Format(f fmt.State, verb rune) {
	_, wide := f.Width()
	_, precise := f.Precision()
	if (verb == 'v' || verb == 's') && !wide && !precise && !f.Flag('-') && !f.Flag('+') && !f.Flag('#') && !f.Flag(' ') && !f.Flag('0') {
		if a.t.IsZero() {
			// The zero Text is formatted as "" would be.
			return
		}
		*a.written++
		_, _ = io.WriteString(f, argStart+strconv.Itoa(a.index)+argEnd)
		return
	}
	_, _ = fmt.Fprintf(f, fmt.FormatString(f, verb), a.t.Plain())
}

// markup reads s as the markup of Colourf. embedded are the texts whose
// marks s holds.
func markup(s string, embedded []Text) Text {
	b := builder{embedded: embedded}
	tokenize(s, b.process)
	return b.text()
}

// builder builds the text of markup from its tokens.
type builder struct {
	embedded []Text
	// formats are the codes of the open tags, outermost first.
	formats []Code
	started bool
	// children are the children of the text; plain holds text outside of
	// tags not yet added to them.
	children []Text
	plain    strings.Builder
	// styled is set if the last piece of text was within tags.
	styled bool
}

// process adds a token to the text.
func (b *builder) process(t token) {
	b.started = true
	switch t.kind {
	case textToken:
		b.write(t.data)
	case startTagToken:
		if c, ok := tags[t.data]; ok {
			b.formats = append(b.formats, c)
			return
		}
		b.write("<" + t.data + ">")
	case endTagToken:
		if c, ok := tags[t.data]; ok && removeFirst(&b.formats, c) {
			return
		}
		b.write("</" + t.data + ">")
	}
}

// write adds a piece of text within the open tags.
func (b *builder) write(s string) {
	if len(b.formats) == 0 {
		b.styled = false
		if b.embedded == nil {
			b.plain.WriteString(s)
			return
		}
		for _, p := range b.split(s) {
			if p.IsZero() {
				continue
			}
			if l, ok := p.Literal(); ok && p.n.style == (Style{}) && len(p.n.children) == 0 {
				b.plain.WriteString(l)
				continue
			}
			b.flush()
			b.children = append(b.children, p)
		}
		return
	}
	b.flush()
	b.styled = true
	inner := &node{kind: literalContent, text: s}
	if b.embedded != nil && strings.Contains(s, argStart) {
		inner = &node{children: b.split(s)}
	}
	inner.style = Style{}.with(b.formats[len(b.formats)-1])
	for i := len(b.formats) - 2; i >= 0; i-- {
		inner = &node{style: Style{}.with(b.formats[i]), children: []Text{{inner}}}
	}
	b.children = append(b.children, Text{inner})
}

// split splits s at the marks of embedded texts into literal text and the
// texts.
func (b *builder) split(s string) []Text {
	var parts []Text
	lit := 0
	for i := 0; ; {
		j := strings.Index(s[i:], argStart)
		if j < 0 {
			break
		}
		start := i + j
		i = start + len(argStart)
		k := strings.Index(s[i:], argEnd)
		if k < 0 {
			break
		}
		index, err := strconv.Atoi(s[i : i+k])
		if err != nil || index < 0 || index >= len(b.embedded) {
			// Colourf writes no such mark, so this is not one.
			continue
		}
		if start > lit {
			parts = append(parts, Literal(s[lit:start]))
		}
		if t := b.embedded[index]; !t.IsZero() {
			parts = append(parts, t)
		}
		i += k + len(argEnd)
		lit = i
	}
	if lit < len(s) || len(parts) == 0 {
		parts = append(parts, Literal(s[lit:]))
	}
	return parts
}

// flush adds the text outside of tags held back as a child.
func (b *builder) flush() {
	if b.plain.Len() > 0 {
		b.children = append(b.children, Literal(b.plain.String()))
		b.plain.Reset()
	}
}

// text returns the text built.
func (b *builder) text() Text {
	if !b.started {
		return Text{}
	}
	b.flush()
	if b.styled {
		b.children = append(b.children, Literal(""))
	}
	return Text{&node{style: Style{Reset: true}, children: b.children}}
}

// tags are the tags of Colourf and the codes they stand for.
var tags = map[string]Code{
	"black":       {Colour: Black},
	"dark-blue":   {Colour: DarkBlue},
	"dark-green":  {Colour: DarkGreen},
	"dark-aqua":   {Colour: DarkAqua},
	"dark-red":    {Colour: DarkRed},
	"dark-purple": {Colour: DarkPurple},
	"orange":      {Colour: Orange},
	"grey":        {Colour: Grey},
	"dark-grey":   {Colour: DarkGrey},
	"blue":        {Colour: Blue},
	"green":       {Colour: Green},
	"aqua":        {Colour: Aqua},
	"red":         {Colour: Red},
	"purple":      {Colour: Purple},
	"yellow":      {Colour: Yellow},
	"white":       {Colour: White},
	"dark-yellow": {Colour: DarkYellow},
	"quartz":      {Colour: Quartz},
	"iron":        {Colour: Iron},
	"netherite":   {Colour: Netherite},
	"obfuscated":  {Decoration: Obfuscated},
	"bold":        {Decoration: Bold},
	"b":           {Decoration: Bold},
	"redstone":    {Colour: Redstone},
	"copper":      {Colour: Copper},
	"gold":        {Colour: Gold},
	"emerald":     {Colour: Emerald},
	"italic":      {Decoration: Italic},
	"i":           {Decoration: Italic},
	"diamond":     {Colour: Diamond},
	"lapis":       {Colour: Lapis},
	"amethyst":    {Colour: Amethyst},
	"resin":       {Colour: Resin},
}

// removeFirst removes the first c of the stack, the outermost, and reports
// whether it was there.
func removeFirst(stack *[]Code, c Code) bool {
	for i, f := range *stack {
		if f == c {
			*stack = append((*stack)[:i], (*stack)[i+1:]...)
			return true
		}
	}
	return false
}

// The tokenizer below reads markup as the tokenizer of HTML does. It is
// the tokenizer of the net/bedrock/text package of the game, which the
// game may not import, and must stay the same as it for Colourf to stay the
// same: the tests of protocol/bedrock/text compare the two.

// Kinds of the tokens of a text: otherToken stands for comments, doctypes
// and self-closing tags.
const (
	textToken = iota
	startTagToken
	endTagToken
	otherToken
)

// token is a token of HTML: text, unescaped, or the name of a tag in lower
// case.
type token struct {
	kind int
	data string
}

// rawTags are the elements whose content is text up to their end tag;
// those of rcdataTags have their character references unescaped.
var (
	rawTags    = map[string]bool{"iframe": true, "noembed": true, "noframes": true, "noscript": true, "plaintext": true, "script": true, "style": true, "textarea": true, "title": true, "xmp": true}
	rcdataTags = map[string]bool{"textarea": true, "title": true}
)

// tokenize reads s as the tokenizer of HTML does and hands its tokens to
// f. A tag cut off by the end is dropped.
func tokenize(s string, f func(token)) {
	var text strings.Builder
	flush := func() {
		if text.Len() > 0 {
			f(token{textToken, newlines(html.UnescapeString(text.String()))})
			text.Reset()
		}
	}
	for len(s) > 0 {
		i := strings.IndexByte(s, '<')
		if i < 0 {
			text.WriteString(s)
			break
		}
		text.WriteString(s[:i])
		s = s[i:]
		if len(s) < 2 {
			// A bracket at the end is text.
			text.WriteString(s)
			break
		}
		c := s[1]
		switch {
		case isLetter(c):
			name, selfClosing, n := readTag(s[1:])
			if n < 0 {
				// The end cut the tag off.
				flush()
				return
			}
			s = s[1+n:]
			if selfClosing {
				flush()
				f(token{kind: otherToken})
				continue
			}
			flush()
			f(token{startTagToken, name})
			if rawTags[name] {
				var (
					raw    string
					closed bool
				)
				raw, s, closed = rawText(s, name)
				if raw != "" {
					if rcdataTags[name] {
						raw = html.UnescapeString(raw)
					}
					f(token{textToken, strings.ReplaceAll(newlines(raw), "\x00", "\uFFFD")})
				}
				if closed {
					f(token{endTagToken, name})
				}
			}
		case c == '/':
			if len(s) < 3 {
				// "</" at the end is text of its own.
				flush()
				f(token{textToken, s})
				return
			}
			flush()
			switch {
			case isLetter(s[2]):
				name, _, n := readTag(s[2:])
				if n < 0 {
					return
				}
				s = s[2+n:]
				f(token{endTagToken, name})
			case s[2] == '>':
				// Dropped, as a comment would be.
				s = s[3:]
				f(token{kind: otherToken})
			default:
				// A bogus comment, up to the next '>' or the end.
				s, _ = skipPast(s[2:], ">")
				f(token{kind: otherToken})
			}
		case c == '!':
			flush()
			// A comment, a doctype or a bogus comment, which the end of s
			// ends too.
			if strings.HasPrefix(s, "<!--") {
				s, _ = skipComment(s[4:])
			} else {
				s, _ = skipPast(s[2:], ">")
			}
			f(token{kind: otherToken})
		case c == '?':
			flush()
			s, _ = skipPast(s[2:], ">")
			f(token{kind: otherToken})
		default:
			text.WriteByte('<')
			s = s[1:]
		}
	}
	flush()
}

// readTag reads the name and attributes of a tag after its '<' or '</':
// its name in lower case, whether it closes itself, and the bytes up to and
// past its '>', -1 if the end of s cut it off. Attributes are read as HTML
// reads them: a quote starts a value only after '='.
func readTag(s string) (name string, selfClosing bool, n int) {
	i := 0
	for i < len(s) && !isSpace(s[i]) && s[i] != '/' && s[i] != '>' {
		i++
	}
	name = asciiLower(s[:i])
	for {
		for i < len(s) && isSpace(s[i]) {
			i++
		}
		if i >= len(s) {
			return "", false, -1
		}
		switch s[i] {
		case '>':
			return name, false, i + 1
		case '/':
			if i+1 < len(s) && s[i+1] == '>' {
				return name, true, i + 2
			}
			i++
			continue
		}
		// The name of an attribute, a first '=' included.
		i++
		for i < len(s) && !isSpace(s[i]) && s[i] != '/' && s[i] != '>' && s[i] != '=' {
			i++
		}
		for i < len(s) && isSpace(s[i]) {
			i++
		}
		if i >= len(s) || s[i] != '=' {
			continue
		}
		i++
		for i < len(s) && isSpace(s[i]) {
			i++
		}
		if i < len(s) && (s[i] == '"' || s[i] == '\'') {
			end := strings.IndexByte(s[i+1:], s[i])
			if end < 0 {
				return "", false, -1
			}
			i += end + 2
			continue
		}
		for i < len(s) && !isSpace(s[i]) && s[i] != '>' {
			i++
		}
	}
}

// rawText returns the text of a raw element up to its end tag, what
// follows the end tag, and whether there was one.
func rawText(s, name string) (string, string, bool) {
	if name == "plaintext" {
		return s, "", false
	}
	for off := 0; ; {
		i := indexEndTag(s[off:], name)
		if i < 0 {
			return s, "", false
		}
		i += off
		j := i + 2 + len(name)
		if j == len(s) || isSpace(s[j]) || s[j] == '/' || s[j] == '>' {
			_, _, n := readTag(s[i+2:])
			if n < 0 {
				return s[:i], "", false
			}
			return s[:i], s[i+2+n:], true
		}
		off = j
	}
}

// newlines turns the line breaks of s into "\n", as HTML reads "\r\n" and
// "\r".
func newlines(s string) string {
	if !strings.Contains(s, "\r") {
		return s
	}
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
}

// indexEndTag returns where "</" and name start in s, the letters of the
// name in any case, -1 if they do not. It compares bytes as ASCII, so
// that no index moves, as one into a string lowered as Unicode would.
func indexEndTag(s, name string) int {
	for i := 0; i+2+len(name) <= len(s); i++ {
		if s[i] != '<' || s[i+1] != '/' {
			continue
		}
		match := true
		for j := 0; j < len(name); j++ {
			c := s[i+2+j]
			if c >= 'A' && c <= 'Z' {
				c += 'a' - 'A'
			}
			if c != name[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// skipPast returns what follows the first sep of s, and false if there is
// none.
func skipPast(s, sep string) (string, bool) {
	_, after, ok := strings.Cut(s, sep)
	return after, ok
}

// skipComment returns what follows the comment whose body s starts with,
// and false if the end cut it off.
func skipComment(s string) (string, bool) {
	// "<!-->" and "<!--->" end at once.
	if strings.HasPrefix(s, ">") {
		return s[1:], true
	}
	if strings.HasPrefix(s, "->") {
		return s[2:], true
	}
	for i := 0; i < len(s); i++ {
		switch {
		case strings.HasPrefix(s[i:], "-->"):
			return s[i+3:], true
		case strings.HasPrefix(s[i:], "--!>"):
			return s[i+4:], true
		}
	}
	return "", false
}

// asciiLower lowers the ASCII letters of s alone, as HTML lowers the names
// of tags.
func asciiLower(s string) string {
	for i := 0; i < len(s); i++ {
		if c := s[i]; c >= 'A' && c <= 'Z' {
			b := []byte(s)
			for j := i; j < len(b); j++ {
				if b[j] >= 'A' && b[j] <= 'Z' {
					b[j] += 'a' - 'A'
				}
			}
			return string(b)
		}
	}
	return s
}

func isLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
func isSpace(c byte) bool  { return c == ' ' || c == '\t' || c == '\n' || c == '\f' || c == '\r' }
