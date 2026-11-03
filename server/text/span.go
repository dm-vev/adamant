package text

import (
	"iter"
	"sync"
)

// Code is one formatting code of a stream of them: a colour, or one
// decoration turned on.
type Code struct {
	// Colour is the colour the code sets, NoColour if the code turns a
	// decoration on.
	Colour Colour
	// Decoration is the decoration the code turns on, a single one, if
	// Colour is NoColour.
	Decoration Decoration
}

// Span is a step of a text written as a stream of formatting codes and
// text, as a legacy string with § codes or a terminal shows text: the
// formatting in effect is reset if Reset is set, the codes are applied in
// order, then the text is written in the formatting in effect.
type Span struct {
	// Reset reports whether the formatting in effect is reset before the
	// codes are applied: no colour and no decorations are in effect after.
	Reset bool
	// Codes are the codes applied before the text. A colour replaces the
	// colour in effect, and a decoration stays on until a reset. The slice
	// is reused: it holds the codes until the next span is produced or the
	// iteration ends.
	Codes []Code
	// Text is the literal text written in the formatting in effect.
	Text string
	// Translation is a translation written in the formatting in effect in
	// place of Text, if it is not the zero Text. Its arguments are not
	// part of the stream. Spans yields translations so only if
	// SpanOptions.Translations asks for it.
	Translation Text
}

// SpanOptions are the options of Spans.
type SpanOptions struct {
	// Decorations are the decorations the stream can show. Others write no
	// codes, so that they cause no reset either.
	Decorations Decoration
	// Translations reports whether a translation is yielded as a span of
	// its own, with no text and Translation set. If it is nil or returns
	// false, the fallback of the translation is written, with its
	// arguments filled in as children of the translation.
	Translations func(t Text) bool
}

// Spans returns t as a stream of spans: the formatting codes and text a
// stream that applies codes in order, such as a legacy string with § codes
// or a terminal, writes to show t. It is the one encoding of text as such
// a stream, which renderers write in their own codes. Codes are written
// before each piece of literal text, empty pieces too, and before each
// translation yielded as a span, by these rules:
//
//   - The codes of a style are its colour, if it has one, then the
//     decorations it turns on that the stream can show, in their order.
//   - The codes of the texts a piece is within are written from the
//     outermost on, as far as they are not in effect yet: codes once written
//     stay in effect for the pieces that follow within the same texts.
//   - A reset comes first, and all those codes are written again, if a text
//     whose codes were written has ended since, or if the piece is within a
//     text that turns off a decoration on in the text around it and codes
//     have been written since the last reset. The codes of such a text are
//     those of the whole style in effect for it; those of the texts around
//     it are not written within it.
//   - A text with Reset writes a reset as soon as it begins, even if no
//     piece follows. The codes of the texts around it are not written
//     within it.
//
// So written, every Text that Colourf returns gives the string of § codes
// the markup gives, and every Text that ParseLegacy returns the string it
// read.
func (t Text) Spans(o SpanOptions) iter.Seq[Span] {
	return func(yield func(Span) bool) {
		if t.n != nil {
			spans(t.n, o, yield)
		}
	}
}

// spans yields the spans of n. It hands yield down rather than keep it, so
// that the loop of a caller that ranges over Spans need not move to the
// heap, and takes its writer from a pool.
func spans(n *node, o SpanOptions, yield func(Span) bool) {
	w := writers.Get().(*spanWriter)
	w.o = o
	w.o.Decorations &= AllDecorations
	w.frames, w.codes = w.frameBuf[:0], w.codeBuf[:0]
	w.base, w.sBase, w.applied = 0, 0, 0
	w.stale, w.clean, w.stopped = false, true, false
	w.walk(n, yield)
	if cap(w.frames) > len(w.frameBuf) || cap(w.codes) > len(w.codeBuf) {
		// Texts nested deeper than most leave their writer to the
		// collector.
		return
	}
	w.o = SpanOptions{}
	writers.Put(w)
}

// writers are the writers spans takes.
var writers = sync.Pool{New: func() any { return new(spanWriter) }}

// frame is a text that Spans is within.
type frame struct {
	// colour and decorations are what the codes of the frame set: those of
	// its style, or, for a frame that cuts, the style in effect for its
	// text if it turns a decoration off.
	colour      Colour
	decorations Decoration
	// effect is the style in effect for the text of the frame: a colour and
	// decorations on, no more than the stream can show.
	effect Style
	// cut is set if the codes for the text of the frame are written from
	// the frame on, and not from its parents.
	cut bool
	// base is the base of the parent of the frame.
	base int
}

// spanWriter writes a text as spans. The codes in effect in the stream are
// those of the frames [sBase, applied), unless the stream is stale.
type spanWriter struct {
	o SpanOptions

	frames   []frame
	frameBuf [16]frame
	codes    []Code
	codeBuf  [32]Code

	// base is the frame the codes of the text of the current frame are
	// written from: the deepest that cuts, or the first.
	base int
	// sBase and applied are the frames the codes in effect were written
	// for: the codes of the frames [sBase, applied).
	sBase, applied int
	// stale is set if codes are in effect that the frames on the stack do
	// not account for, as those of a frame that has ended.
	stale bool
	// clean is set if no codes have been written since the last reset.
	clean bool
	// stopped is set once yield has returned false.
	stopped bool
}

// walk writes the text n and its children. It returns false once the
// consumer of the spans stops.
func (w *spanWriter) walk(n *node, yield func(Span) bool) bool {
	w.push(n.style, yield)
	if w.stopped {
		return false
	}
	switch n.kind {
	case literalContent:
		if !w.write(n.text, Text{}, yield) {
			return false
		}
	case translationContent:
		if t := (Text{n}); w.o.Translations != nil && w.o.Translations(t) {
			if !w.write("", t, yield) {
				return false
			}
			break
		}
		n.fill(func(s string) {
			if !w.stopped {
				w.write(s, Text{}, yield)
			}
		}, func(a *node) {
			if !w.stopped {
				w.walk(a, yield)
			}
		})
		if w.stopped {
			return false
		}
	}
	for _, c := range n.children {
		if c.n != nil && !w.walk(c.n, yield) {
			return false
		}
	}
	w.pop()
	return true
}

// push enters a text of the style s.
func (w *spanWriter) push(s Style, yield func(Span) bool) {
	i := len(w.frames)
	var parent Style
	if i > 0 {
		parent = w.frames[i-1].effect
	}
	f := frame{base: w.base, effect: s.Inherit(parent)}
	f.effect.On &= w.o.Decorations
	switch {
	case s.Reset:
		f.cut = true
		f.colour, f.decorations = s.Colour, s.On&w.o.Decorations
	case s.Off&parent.On != 0:
		f.cut = true
		f.colour, f.decorations = f.effect.Colour, f.effect.On
	default:
		f.colour, f.decorations = s.Colour, s.On&w.o.Decorations
	}
	w.frames = append(w.frames, f)
	if !f.cut {
		return
	}
	w.base = i
	if s.Reset {
		w.stale, w.clean = false, true
		w.sBase, w.applied = i, i
		w.emit(Span{Reset: true}, yield)
	}
}

// pop leaves the text entered last.
func (w *spanWriter) pop() {
	i := len(w.frames) - 1
	f := w.frames[i]
	w.frames = w.frames[:i]
	if f.cut {
		w.base = f.base
		if w.sBase >= i {
			// The codes in effect were written within the frame.
			if w.clean {
				w.sBase, w.applied = w.base, w.base
			} else {
				w.stale = true
			}
		}
		return
	}
	if !w.stale && w.applied > i {
		if f.colour != NoColour || f.decorations != 0 {
			w.stale = true
		} else {
			w.applied = i
		}
	}
}

// write writes literal text, or the translation tr if it is not zero, in
// the style of the current frame.
func (w *spanWriter) write(s string, tr Text, yield func(Span) bool) bool {
	n := len(w.frames)
	reset, from := false, w.applied
	switch {
	case w.stale:
		reset, from = true, w.base
	case w.clean:
		from = w.base
	case w.sBase != w.base:
		reset, from = true, w.base
	}
	w.codes = w.codes[:0]
	for _, f := range w.frames[from:n] {
		if f.colour != NoColour {
			w.codes = append(w.codes, Code{Colour: f.colour})
		}
		for d := Obfuscated; d <= Italic; d <<= 1 {
			if f.decorations&d != 0 {
				w.codes = append(w.codes, Code{Decoration: d})
			}
		}
	}
	if reset {
		w.stale, w.clean = false, true
	}
	if len(w.codes) > 0 {
		w.clean = false
	}
	w.sBase, w.applied = w.base, n
	if !reset && len(w.codes) == 0 && s == "" && tr.IsZero() {
		return true
	}
	return w.emit(Span{Reset: reset, Codes: w.codes, Text: s, Translation: tr}, yield)
}

// emit yields a span and reports whether the consumer wants more.
func (w *spanWriter) emit(s Span, yield func(Span) bool) bool {
	if !yield(s) {
		w.stopped = true
	}
	return !w.stopped
}
