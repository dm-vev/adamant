package text

import (
	"iter"
	"slices"
	"strconv"
	"strings"
)

// Text is a piece of rich text: a component with content of its own,
// literal text or a translation, a Style, and children that follow the
// content and inherit the style. Text is immutable: the methods that
// change a text return a new one and leave the text they are called on as
// it was, and texts can be shared and passed around freely, across
// goroutines too.
//
// The zero Text is empty: it has no content, no style and no children, and
// renders as nothing. Texts are compared with Equal or Equivalent; ==
// compares whether two texts are the same value, not whether they hold the
// same text.
type Text struct {
	n *node
}

// node is a text. It is never changed once a Text holds it.
type node struct {
	kind  kind
	style Style
	// text is the literal text, or the key of a translation.
	text     string
	tr       *translation
	children []Text
	// legacy is the string ParseLegacy read the text from, if the text is
	// one ParseLegacy returned: Legacy writes it as it is rather than walk
	// the text, since the text renders as the string. A text built from
	// the node, by WithStyle or Append, does not have it. ParseLegacy
	// never returns a node for the empty string, so the string is empty
	// for every other text.
	legacy string
}

// kind is the kind of the content of a text.
type kind uint8

const (
	noContent kind = iota
	literalContent
	translationContent
)

// translation holds the fallback and the arguments of a translation.
type translation struct {
	fallback string
	args     []Text
}

// Literal returns literal text: s exactly as it is, with no style. The
// model reads no markup or formatting codes in it; a § code in s stays
// part of the text, which a client shown it may still read as a code, so
// strings that carry codes are read with ParseLegacy instead.
func Literal(s string) Text {
	return Text{&node{kind: literalContent, text: s}}
}

// Translate returns a translation: text a client shows in its own language
// by looking key up, with the arguments filled in. The key is that of the
// language files, without the % Bedrock Edition puts before a key in a
// message.
//
// fallback is the text shown where the key cannot be looked up, such as
// on the console, or by a client that does not know it. Like the strings
// of the language files, it is a format: %s takes the next argument, %n$s
// the nth, and %% is a percent sign. A fallback with any other %, or one
// that refers to an argument not passed, is shown as it is; an empty
// fallback shows the key. Zero arguments are skipped where they are
// filled in.
func Translate(key, fallback string, args ...Text) Text {
	tr := &translation{fallback: fallback}
	if len(args) > 0 {
		tr.args = slices.Clone(args)
	}
	return Text{&node{kind: translationContent, text: key, tr: tr}}
}

// Concat returns the texts passed one after another, as the children of a
// text with no content and no style. It skips zero texts; it returns the
// zero Text if no other is left, and a text left alone as it is.
func Concat(parts ...Text) Text {
	children := make([]Text, 0, len(parts))
	for _, p := range parts {
		if !p.IsZero() {
			children = append(children, p)
		}
	}
	switch len(children) {
	case 0:
		return Text{}
	case 1:
		return children[0]
	}
	return Text{&node{children: children}}
}

// IsZero reports whether t is the zero Text. A text of empty literal
// content is not zero: it has content, which a renderer of formatting
// codes writes the codes of its style before.
func (t Text) IsZero() bool { return t.n == nil }

// Style returns the style of t.
func (t Text) Style() Style {
	if t.n == nil {
		return Style{}
	}
	return t.n.style
}

// WithStyle returns t with the style s in place of its own. Off loses the
// decorations of On, and all of them with Reset; a colour that is none of
// those of the package becomes NoColour. On the zero Text it returns a
// text with no content and the style s.
func (t Text) WithStyle(s Style) Text {
	n := node{}
	if t.n != nil {
		n = *t.n
	}
	n.style, n.legacy = s.normalised(), ""
	return Text{&n}
}

// Append returns t with the texts passed added after its children. It
// skips zero texts. On the zero Text it is Concat.
func (t Text) Append(children ...Text) Text {
	if t.n == nil {
		return Concat(children...)
	}
	n := *t.n
	n.legacy = ""
	n.children = slices.Clip(n.children)
	for _, c := range children {
		if !c.IsZero() {
			n.children = append(n.children, c)
		}
	}
	return Text{&n}
}

// Literal returns the literal content of t, if t is literal text. It does
// not include the content of the children of t.
func (t Text) Literal() (s string, ok bool) {
	if t.n == nil || t.n.kind != literalContent {
		return "", false
	}
	return t.n.text, true
}

// Translation returns the key and the fallback of t, if t is a
// translation.
func (t Text) Translation() (key, fallback string, ok bool) {
	if t.n == nil || t.n.kind != translationContent {
		return "", "", false
	}
	return t.n.text, t.n.tr.fallback, true
}

// Args returns the arguments of t, if t is a translation.
func (t Text) Args() iter.Seq[Text] {
	return func(yield func(Text) bool) {
		if t.n == nil || t.n.tr == nil {
			return
		}
		for _, a := range t.n.tr.args {
			if !yield(a) {
				return
			}
		}
	}
}

// Children returns the children of t, in order.
func (t Text) Children() iter.Seq[Text] {
	return func(yield func(Text) bool) {
		if t.n == nil {
			return
		}
		for _, c := range t.n.children {
			if !yield(c) {
				return
			}
		}
	}
}

// Plain returns the text of t without its style: the literal text of t
// and its children in order, translations replaced by their fallbacks with
// the arguments filled in. § codes in literal text are text: they are kept.
func (t Text) Plain() string {
	if t.n == nil {
		return ""
	}
	if t.n.kind == literalContent && len(t.n.children) == 0 {
		return t.n.text
	}
	var b strings.Builder
	t.n.appendPlain(&b)
	return b.String()
}

// appendPlain writes the plain text of n to b.
func (n *node) appendPlain(b *strings.Builder) {
	switch n.kind {
	case literalContent:
		b.WriteString(n.text)
	case translationContent:
		n.fill(func(s string) { b.WriteString(s) }, func(a *node) { a.appendPlain(b) })
	}
	for _, c := range n.children {
		if c.n != nil {
			c.n.appendPlain(b)
		}
	}
}

// String returns the plain text of t, as Plain does.
func (t Text) String() string { return t.Plain() }

// fill calls text for the literal parts of the fallback of the translation
// n, the key if the fallback is empty, and arg for the arguments it refers
// to, in order.
func (n *node) fill(text func(string), arg func(*node)) {
	f := n.tr.fallback
	if f == "" {
		text(n.text)
		return
	}
	if !validFormat(f, len(n.tr.args)) {
		text(f)
		return
	}
	next := 0
	for f != "" {
		i := strings.IndexByte(f, '%')
		if i < 0 {
			text(f)
			return
		}
		if i > 0 {
			text(f[:i])
		}
		index, size, percent := placeholder(f[i:])
		f = f[i+size:]
		switch {
		case percent:
			text("%")
		default:
			if index < 0 {
				index = next
				next++
			}
			if a := n.tr.args[index]; a.n != nil {
				arg(a.n)
			}
		}
	}
}

// validFormat reports whether f is a format of a translation with n
// arguments that can be filled in: every % starts a placeholder, and every
// placeholder refers to an argument there is. It follows the language
// files of Java Edition, which show a format that is not valid as it is.
func validFormat(f string, n int) bool {
	next := 0
	for {
		i := strings.IndexByte(f, '%')
		if i < 0 {
			return true
		}
		index, size, percent := placeholder(f[i:])
		if size == 0 {
			return false
		}
		f = f[i+size:]
		if percent {
			continue
		}
		if index < 0 {
			index = next
			next++
		}
		if index >= n {
			return false
		}
	}
}

// placeholder reads the placeholder f starts with, at its %: %s, %n$s or
// %%. It returns the index of the argument of %n$s, counted from zero, -1
// for %s, the size of the placeholder, and whether it is %%. A size of 0
// means f starts with no placeholder. The index of %0$s, which refers to no
// argument, and indexes too great to hold are returned as the greatest
// int, which no argument has.
func placeholder(f string) (index, size int, percent bool) {
	if len(f) < 2 {
		return 0, 0, false
	}
	switch f[1] {
	case 's':
		return -1, 2, false
	case '%':
		return 0, 2, true
	}
	i := 1
	for i < len(f) && f[i] >= '0' && f[i] <= '9' {
		i++
	}
	if i == 1 || i+1 >= len(f) || f[i] != '$' || f[i+1] != 's' {
		return 0, 0, false
	}
	n, err := strconv.Atoi(f[1:i])
	if err != nil || n == 0 {
		return int(^uint(0) >> 1), i + 2, false
	}
	return n - 1, i + 2, false
}

// Equal reports whether a and b are the same text: the same content, style
// and children, in the same structure. Two texts that look the same but
// are built differently, such as a text with the colour red and a text of
// no style with a red child, are not equal; Equivalent compares how texts
// look.
func Equal(a, b Text) bool {
	if a.n == b.n {
		return true
	}
	if a.n == nil || b.n == nil {
		return false
	}
	x, y := a.n, b.n
	if x.kind != y.kind || x.style != y.style || x.text != y.text || len(x.children) != len(y.children) {
		return false
	}
	if x.kind == translationContent {
		if x.tr.fallback != y.tr.fallback || len(x.tr.args) != len(y.tr.args) {
			return false
		}
		for i := range x.tr.args {
			if !Equal(x.tr.args[i], y.tr.args[i]) {
				return false
			}
		}
	}
	for i := range x.children {
		if !Equal(x.children[i], y.children[i]) {
			return false
		}
	}
	return true
}

// Equivalent reports whether a and b look the same where text has no
// style of its own, such as in the chat: they have the same text in the
// same colours and decorations, and the same translations, in the same
// order. How their texts are split, nested and styled to look so does not
// matter. Translations are compared, not filled in: their keys and
// fallbacks are the same, and their arguments are equivalent in the style
// the translations have.
func Equivalent(a, b Text) bool {
	return equivalentRuns(runsOf(a, Style{}), runsOf(b, Style{}))
}

// equivalentRuns reports whether the runs xs and ys look the same.
func equivalentRuns(xs, ys []run) bool {
	return slices.EqualFunc(xs, ys, func(x, y run) bool {
		if x.style != y.style || x.text != y.text || (x.tr == nil) != (y.tr == nil) {
			return false
		}
		if x.tr == nil {
			return true
		}
		if x.tr.text != y.tr.text || x.tr.tr.fallback != y.tr.tr.fallback || len(x.tr.tr.args) != len(y.tr.tr.args) {
			return false
		}
		for i, arg := range x.tr.tr.args {
			if !equivalentRuns(runsOf(arg, x.style), runsOf(y.tr.tr.args[i], y.style)) {
				return false
			}
		}
		return true
	})
}

// run is literal text in the style in effect for it, or a translation.
type run struct {
	style Style
	text  string
	tr    *node
}

// runsOf returns the runs of t, inheriting the style in effect inherited:
// literal text is joined where the style in effect stays the same, and
// empty text left out.
func runsOf(t Text, inherited Style) []run {
	var runs []run
	var walk func(n *node, parent Style)
	walk = func(n *node, parent Style) {
		s := n.style.Inherit(parent)
		switch n.kind {
		case literalContent:
			if n.text == "" {
				break
			}
			if l := len(runs) - 1; l >= 0 && runs[l].tr == nil && runs[l].style == s {
				runs[l].text += n.text
				break
			}
			runs = append(runs, run{style: s, text: n.text})
		case translationContent:
			runs = append(runs, run{style: s, tr: n})
		}
		for _, c := range n.children {
			if c.n != nil {
				walk(c.n, s)
			}
		}
	}
	if t.n != nil {
		walk(t.n, inherited)
	}
	return runs
}

// GoString returns t as it is built, for the messages of tests and
// debugging: each text in braces, with its style (reset, the colour,
// +decoration for those on and -decoration for those off), its content,
// literal text quoted or a translation as tr(key, fallback, arguments...),
// and its children. The zero Text is {}.
func (t Text) GoString() string {
	var b strings.Builder
	t.appendDebug(&b)
	return b.String()
}

// appendDebug writes t as GoString returns it to b.
func (t Text) appendDebug(b *strings.Builder) {
	b.WriteByte('{')
	if t.n == nil {
		b.WriteByte('}')
		return
	}
	var parts []string
	s := t.n.style
	if s.Reset {
		parts = append(parts, "reset")
	}
	if s.Colour != NoColour {
		parts = append(parts, s.Colour.String())
	}
	for i, name := range decorationNames {
		switch d := Decoration(1 << i); {
		case s.On&d != 0:
			parts = append(parts, "+"+name)
		case s.Off&d != 0:
			parts = append(parts, "-"+name)
		}
	}
	b.WriteString(strings.Join(parts, " "))
	sep := func() {
		if len(parts) > 0 {
			b.WriteByte(' ')
		}
		parts = append(parts, "")
	}
	switch t.n.kind {
	case literalContent:
		sep()
		b.WriteString(strconv.Quote(t.n.text))
	case translationContent:
		sep()
		b.WriteString("tr(")
		b.WriteString(strconv.Quote(t.n.text))
		b.WriteString(", ")
		b.WriteString(strconv.Quote(t.n.tr.fallback))
		for _, a := range t.n.tr.args {
			b.WriteString(", ")
			a.appendDebug(b)
		}
		b.WriteByte(')')
	}
	for _, c := range t.n.children {
		sep()
		c.appendDebug(b)
	}
	b.WriteByte('}')
}
