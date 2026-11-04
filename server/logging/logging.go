// Package logging gives the components of the server loggers of their own:
// each has a level, and all write to one output, as text or as JSON.
//
// A Set holds the level of each component and the output they share. The
// logger of a component comes from Set.Logger, or from Sub of a logger a
// Set made, which keeps the attributes the logger was made with. There is
// no logger of the package: each part of the server takes its logger from
// its config, as it did before, and scopes it to its component with Sub. A
// logger made elsewhere, as an embedder's, passes through Sub unchanged.
//
// The text of a Set is the text of the default handler of slog, byte for
// byte: "2006/01/02 15:04:05 LEVEL msg k=v", written through a log.Logger
// as that handler writes through log.Default(), with the same timestamp and
// the same lock as log.Print. For that, a Set of the format Text must never
// become the default logger of slog: slog.SetDefault of its logger makes
// log.Default() write through the Set, which writes through log.Default(),
// and the second write waits for the lock of the first for ever. The JSON
// of a Set is that of slog.JSONHandler with a "component" field, and may be
// the default.
package logging

import (
	"bytes"
	"context"
	"io"
	"log"
	"log/slog"
	"math"
	"os"
	"slices"
	"sync"
	"time"
	"unsafe"
)

// The components of the server. A Set knows these and has a level for
// each; Sub with another name, as a plugin's, gives the level of the root.
const (
	// Server is the life of the server: its start and close, the config,
	// the lists of admins and the identity of players.
	Server = "server"
	// World is the ticking of the worlds, their chunks, their data beside
	// the chunks and the game code that runs in them.
	World = "world"
	// Generator is the workers that generate chunks and the structures they
	// place.
	Generator = "generator"
	// Save is the checkpoints, the writes of chunks and the world provider.
	Save = "save"
	// Session is the sessions of players: their packets, forms and chunks.
	Session = "session"
	// Network is the listeners, the logins and the services around them:
	// NetherNet, Xbox Live, LAN, ACME, the proxy and query.
	Network = "network"
	// Player is the players.
	Player = "player"
	// Commands is the console and the commands players run.
	Commands = "commands"
	// AntiCheat is the anti-cheat.
	AntiCheat = "anticheat"
)

// Components returns the names of the components a Set knows, sorted.
func Components() []string {
	return []string{AntiCheat, Commands, Generator, Network, Player, Save, Server, Session, World}
}

// Format is how a Set writes lines.
type Format uint8

const (
	// Text writes lines as the default handler of slog does:
	// "2006/01/02 15:04:05 LEVEL msg k=v".
	Text Format = iota
	// JSON writes one object per line, as slog.JSONHandler does, with the
	// component of the line in a "component" field.
	JSON
)

// String returns "text" or "json".
func (f Format) String() string {
	if f == JSON {
		return "json"
	}
	return "text"
}

// Options configures a Set.
type Options struct {
	// Format is how lines are written.
	Format Format
	// Level is the level of the root, which the components inherit; Levels
	// overrides it for the components it names.
	Level  slog.Level
	Levels map[string]slog.Level
	// Output is where JSON goes, os.Stderr if nil.
	Output io.Writer
	// Logger is what text is written through, log.Default() if nil, as the
	// default handler of slog writes it.
	Logger *log.Logger
}

// Set is the levels of the components of the server and the output they
// share. Its methods may be called at the same time from any goroutine.
type Set struct {
	mu   sync.Mutex // guards the own fields of levels, for SetLevel
	root slog.LevelVar
	// levels holds the level of each component the Set knows, fixed when
	// the Set is made.
	levels map[string]*componentLevel

	format Format
	// text is where the attributes of a line of text are formatted, and
	// logger what the line is written through.
	text   *textSink
	logger *log.Logger
	// json is the JSON handler of the Set, and jsonOf that of each
	// component it knows, with the component's field.
	json   slog.Handler
	jsonOf map[string]slog.Handler
}

// componentLevel is the level of a component: its own if own, and the
// root's otherwise, copied in v so that a line checks one atomic.
type componentLevel struct {
	v   slog.LevelVar
	own bool
}

// New returns a Set of the options passed.
func New(o Options) *Set {
	s := &Set{levels: map[string]*componentLevel{}, format: o.Format, logger: o.Logger}
	s.root.Set(o.Level)
	for _, c := range Components() {
		l := &componentLevel{}
		l.v.Set(o.Level)
		if v, ok := o.Levels[c]; ok {
			l.v.Set(v)
			l.own = true
		}
		s.levels[c] = l
	}
	if s.logger == nil {
		s.logger = log.Default()
	}
	switch o.Format {
	case JSON:
		out := o.Output
		if out == nil {
			out = os.Stderr
		}
		s.json = slog.NewJSONHandler(out, &slog.HandlerOptions{Level: slog.Level(math.MinInt)})
		s.jsonOf = map[string]slog.Handler{}
		for _, c := range Components() {
			s.jsonOf[c] = s.json.WithAttrs([]slog.Attr{slog.String("component", c)})
		}
	default:
		s.text = &textSink{}
		s.text.h = slog.NewTextHandler(s.text, &slog.HandlerOptions{Level: slog.Level(math.MinInt)})
	}
	return s
}

// Format returns the format of the Set.
func (s *Set) Format() Format { return s.format }

// Logger returns the logger of a component.
func (s *Set) Logger(component string) *slog.Logger {
	h := &handler{set: s, comp: component, level: s.levelOf(component)}
	h.inner = s.base(component)
	return slog.New(h)
}

// Level returns the level of a component, or of the root for "".
func (s *Set) Level(component string) slog.Level {
	return s.levelOf(component).Level()
}

// SetLevel sets the level of a component the Set knows, or of the root for
// "", and reports whether it knows the component. The components that
// inherit the level of the root follow it.
func (s *Set) SetLevel(component string, l slog.Level) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if component == "" {
		s.root.Set(l)
		for _, c := range s.levels {
			if !c.own {
				c.v.Set(l)
			}
		}
		return true
	}
	c, ok := s.levels[component]
	if ok {
		c.own = true
		c.v.Set(l)
	}
	return ok
}

// Inherit makes a component the Set knows take the level of the root
// again, and reports whether it knows the component.
func (s *Set) Inherit(component string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.levels[component]
	if ok {
		c.own = false
		c.v.Set(s.root.Level())
	}
	return ok
}

// levelOf returns the level a line of a component checks.
func (s *Set) levelOf(component string) *slog.LevelVar {
	if c, ok := s.levels[component]; ok {
		return &c.v
	}
	return &s.root
}

// base returns the handler that formats the lines of a component with no
// attributes.
func (s *Set) base(component string) slog.Handler {
	if s.format != JSON {
		return s.text.h
	}
	if h, ok := s.jsonOf[component]; ok {
		return h
	}
	return s.json.WithAttrs([]slog.Attr{slog.String("component", component)})
}

// Sub returns l scoped to a component: of its level, and in JSON of its
// field, with the attributes and groups l was made with, if a Set made l.
// Any other logger is returned as it is, so that a logger an embedder
// passes keeps working untouched.
func Sub(l *slog.Logger, component string) *slog.Logger {
	if l == nil {
		return nil
	}
	h, ok := l.Handler().(*handler)
	if !ok || h.comp == component {
		return l
	}
	return slog.New(h.sub(component))
}

// op is a call of WithAttrs, or of WithGroup if attrs is nil, on a logger of
// a Set, kept to make the logger anew for another component.
type op struct {
	attrs []slog.Attr
	group string
}

// handler is the slog.Handler of the loggers of a Set.
type handler struct {
	set   *Set
	comp  string
	level *slog.LevelVar
	// inner formats the attributes: the TextHandler of the Set's sink, or
	// a JSON handler with the component's field, with ops applied.
	inner slog.Handler
	ops   []op
}

// Enabled reports whether the level of the component lets l through: one
// atomic load.
func (h *handler) Enabled(_ context.Context, l slog.Level) bool { return l >= h.level.Level() }

// Handle writes a line.
func (h *handler) Handle(ctx context.Context, r slog.Record) error {
	if h.set.format == JSON {
		return h.inner.Handle(ctx, r)
	}
	return h.set.text.write(ctx, h.set.logger, h.inner, r)
}

// WithAttrs returns the handler with the attributes passed added.
func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	c := *h
	c.inner = h.inner.WithAttrs(attrs)
	c.ops = append(slices.Clip(h.ops), op{attrs: slices.Clone(attrs)})
	return &c
}

// WithGroup returns the handler with a group of the name passed opened.
func (h *handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	c := *h
	c.inner = h.inner.WithGroup(name)
	c.ops = append(slices.Clip(h.ops), op{group: name})
	return &c
}

// sub returns the handler for another component, with the same attributes.
func (h *handler) sub(component string) *handler {
	c := *h
	c.comp = component
	c.level = h.set.levelOf(component)
	if h.set.format == JSON {
		c.inner = h.set.base(component)
		for _, o := range h.ops {
			if o.attrs != nil {
				c.inner = c.inner.WithAttrs(o.attrs)
			} else {
				c.inner = c.inner.WithGroup(o.group)
			}
		}
	}
	return &c
}

// textSink is where the TextHandler of a Set of text writes the attributes
// of a line, under the lock of the sink.
type textSink struct {
	mu  sync.Mutex
	buf []byte
	h   slog.Handler
}

// Write keeps the formatted attributes for write to take.
func (t *textSink) Write(p []byte) (int, error) {
	t.buf = append(t.buf, p...)
	return len(p), nil
}

// textPrefix is how the TextHandler of the sink begins every line: write
// passes it records of no time, of the level info and of no message, so
// that only the attributes vary.
var textPrefix = []byte(`level=INFO msg=""`)

// linePool holds the buffers lines are built in.
var linePool = sync.Pool{New: func() any { b := make([]byte, 0, 256); return &b }}

// write writes the record r as the default handler of slog does: the level,
// a space and the message, then the attributes, through logger. inner is
// the TextHandler of the sink with the attributes of the logger: it formats
// the attributes as the default handler does, both being the text handler
// of slog, and write takes the text after the level and message it writes.
func (t *textSink) write(ctx context.Context, logger *log.Logger, inner slog.Handler, r slog.Record) error {
	bp := linePool.Get().(*[]byte)
	line := (*bp)[:0]
	line = append(line, r.Level.String()...)
	line = append(line, ' ')
	line = append(line, r.Message...)

	attrs := r
	attrs.Time, attrs.Level, attrs.Message, attrs.PC = time.Time{}, slog.LevelInfo, "", 0
	t.mu.Lock()
	t.buf = t.buf[:0]
	err := inner.Handle(ctx, attrs)
	rest := t.buf
	if len(rest) > 0 && rest[len(rest)-1] == '\n' {
		rest = rest[:len(rest)-1]
	}
	if b, ok := bytes.CutPrefix(rest, textPrefix); ok {
		line = append(line, b...)
	}
	t.mu.Unlock()
	if err == nil {
		// log.Logger.Output copies the text and keeps none of it, so the
		// buffer may be reused once it returns.
		err = logger.Output(4, unsafe.String(unsafe.SliceData(line), len(line)))
	}
	*bp = line
	linePool.Put(bp)
	return err
}
