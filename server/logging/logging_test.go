package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// stringer is a fmt.Stringer of a text with spaces and an equals sign.
type stringer struct{}

func (stringer) String() string { return "a b=c" }

// valuer is a slog.LogValuer of a group.
type valuer struct{}

func (valuer) LogValue() slog.Value {
	return slog.GroupValue(slog.Int("x", 1), slog.String("y", "two words"))
}

// corpus logs the lines of a server, and the odd ones, through the logger
// passed.
func corpus(l *slog.Logger) {
	l.Info("Dragonfly server started.", "mc-version", "1.21.130", "go-version", "go1.27.3", "commit", "")
	l.Debug("process packet: *packet.Text", "name", "Steve")
	l.With("dimension", "overworld").Warn("TPS dropped below threshold.", "tps", 18.734)
	l.With("name", "Steve Two", "uuid", "abc", "raddr", "1.2.3.4:5").Error("panic handling a packet",
		"packet", "*packet.Text", "panic", errors.New(`boom "x"`), "stack", "goroutine 1\n\tmain.go:3\n")
	l.Info("Saved checkpoint.", "took", 1500*time.Millisecond, "chunks", 12)
	l.WithGroup("g").Info("grouped", "a", 1, slog.Group("h", "b", 2))
	l.WithGroup("empty").Info("no attributes")
	l.With("k", "v").WithGroup("g").With("w", 3).Info("chain", "z", true)
	l.Info("stringers", "s", stringer{}, "v", valuer{}, "nil", nil, "err", error(nil))
	l.Info("two\nlines", "level", "not the level", "msg", "not the message", "time", "not the time")
	l.Log(context.Background(), slog.LevelError+2, "custom level")
	l.Info("§cServer is white-listed", "§", "§a")
	l.Info("")
	l.Info("trailing newline\n")
}

// withDefaultLog points log.Default() at w with the flags passed, and the
// level of slog's default handler at debug, until the test ends.
func withDefaultLog(t *testing.T, w io.Writer, flags int) {
	t.Helper()
	out, f, lvl := log.Writer(), log.Flags(), slog.SetLogLoggerLevel(slog.LevelDebug)
	log.SetOutput(w)
	log.SetFlags(flags)
	t.Cleanup(func() {
		log.SetOutput(out)
		log.SetFlags(f)
		slog.SetLogLoggerLevel(lvl)
	})
}

// TestTextIsTheDefaultHandlers checks that a Set of text writes what the
// default handler of slog writes, byte for byte, and that Sub of its
// loggers keeps their attributes and writes no component.
func TestTextIsTheDefaultHandlers(t *testing.T) {
	var want, got bytes.Buffer
	withDefaultLog(t, &want, 0)
	if _, ok := slog.Default().Handler().(*handler); ok {
		t.Fatal("the default logger is of a Set")
	}
	corpus(slog.Default())
	corpus(slog.Default().With("name", "Steve").WithGroup("p"))

	s := New(Options{Level: slog.LevelDebug, Logger: log.New(&got, "", 0)})
	corpus(s.Logger(Server))
	corpus(Sub(s.Logger(Session).With("name", "Steve").WithGroup("p"), AntiCheat))
	if got.String() != want.String() {
		t.Errorf("a Set wrote:\n%s\nslog's default handler:\n%s", got.String(), want.String())
	}

	// With the flags of log.Default(), the lines differ only in their
	// timestamps.
	want.Reset()
	got.Reset()
	withDefaultLog(t, &want, log.LstdFlags)
	corpus(slog.Default())
	s = New(Options{Level: slog.LevelDebug, Logger: log.New(&got, "", log.LstdFlags)})
	corpus(s.Logger(World))
	stamp := regexp.MustCompile(`(?m)^\d{4}/\d\d/\d\d \d\d:\d\d:\d\d `)
	if !stamp.MatchString(got.String()) {
		t.Fatalf("no timestamp in:\n%s", got.String())
	}
	if g, w := stamp.ReplaceAllString(got.String(), ""), stamp.ReplaceAllString(want.String(), ""); g != w {
		t.Errorf("a Set wrote:\n%s\nslog's default handler:\n%s", g, w)
	}
}

// TestTextHasNoComponent checks that the component of a line is in its
// JSON once, after Sub of Sub too, and never in its text.
func TestTextHasNoComponent(t *testing.T) {
	var text bytes.Buffer
	s := New(Options{Logger: log.New(&text, "", 0)})
	Sub(Sub(s.Logger(Server), Player), AntiCheat).Info("x", "a", 1)
	if strings.Contains(text.String(), "component") {
		t.Errorf("text with a component: %s", text.String())
	}

	var out bytes.Buffer
	s = New(Options{Format: JSON, Output: &out})
	Sub(Sub(s.Logger(Server).With("name", "Steve"), Player).WithGroup("g"), AntiCheat).Info("x", "a", 1)
	line := out.String()
	if n := strings.Count(line, `"component"`); n != 1 {
		t.Fatalf("component %d times in %s", n, line)
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(line), &v); err != nil {
		t.Fatal(err)
	}
	if v["component"] != AntiCheat || v["name"] != "Steve" || v["msg"] != "x" || v["level"] != "INFO" {
		t.Errorf("JSON line %s", line)
	}
	if g, ok := v["g"].(map[string]any); !ok || g["a"] != float64(1) {
		t.Errorf("JSON line %s: no group g with a", line)
	}
}

// TestJSONIsOneLinePerRecord checks that a stack is a string of one line.
func TestJSONIsOneLinePerRecord(t *testing.T) {
	var out bytes.Buffer
	s := New(Options{Format: JSON, Output: &out})
	s.Logger(Session).Error("panic handling a packet", "stack", "goroutine 1\n\tmain.go:3\n")
	if strings.Count(out.String(), "\n") != 1 {
		t.Errorf("JSON of a stack: %q", out.String())
	}
}

// TestLevelsByComponent checks the levels of components: their own if
// set, the root's otherwise, SetLevel of the root moving only those that
// inherit, and Inherit.
func TestLevelsByComponent(t *testing.T) {
	var out bytes.Buffer
	s := New(Options{Level: slog.LevelInfo, Levels: map[string]slog.Level{Session: slog.LevelDebug, World: slog.LevelError},
		Logger: log.New(&out, "", 0)})
	enabled := func(c string, l slog.Level) bool { return s.Logger(c).Enabled(t.Context(), l) }
	if !enabled(Session, slog.LevelDebug) || enabled(World, slog.LevelWarn) || !enabled(World, slog.LevelError) ||
		enabled(Save, slog.LevelDebug) || !enabled(Save, slog.LevelInfo) || enabled("plugin", slog.LevelDebug) {
		t.Fatal("levels of the options not applied")
	}
	session, save, plugin := s.Logger(Session), s.Logger(Save), s.Logger("plugin")
	s.SetLevel("", slog.LevelWarn)
	if !session.Enabled(t.Context(), slog.LevelDebug) || save.Enabled(t.Context(), slog.LevelInfo) || plugin.Enabled(t.Context(), slog.LevelInfo) {
		t.Fatal("SetLevel of the root moved a component of its own level, or not one that inherits")
	}
	if !s.SetLevel(Save, slog.LevelDebug) || !save.Enabled(t.Context(), slog.LevelDebug) {
		t.Fatal("SetLevel of a component not applied to its logger")
	}
	s.SetLevel("", slog.LevelError)
	if !save.Enabled(t.Context(), slog.LevelDebug) {
		t.Fatal("a component of its own level followed the root")
	}
	if !s.Inherit(Save) || save.Enabled(t.Context(), slog.LevelWarn) || s.Level(Save) != slog.LevelError {
		t.Fatal("Inherit did not give the level of the root back")
	}
	if s.SetLevel("plugin", slog.LevelDebug) {
		t.Fatal("SetLevel of an unknown component")
	}

	// Sub keeps the attributes, and takes the level of its component.
	l := Sub(s.Logger(Session).With("name", "Steve"), World)
	s.SetLevel(World, slog.LevelInfo)
	l.Info("x")
	if out.String() != "INFO x name=Steve\n" {
		t.Errorf("Sub wrote %q", out.String())
	}
}

// TestSubOfForeignLogger checks that Sub returns a logger no Set made as it
// is.
func TestSubOfForeignLogger(t *testing.T) {
	l := slog.New(slog.DiscardHandler)
	if Sub(l, World) != l {
		t.Error("Sub changed a logger of another handler")
	}
	if Sub(nil, World) != nil {
		t.Error("Sub of nil")
	}
	s := New(Options{})
	if w := s.Logger(World); Sub(w, World) != w {
		t.Error("Sub to the same component made a new logger")
	}
}

// TestNoDeadlockWithLogPrint checks that lines of a Set of text and of
// log.Print written at the same time all end, and that, with a Set of JSON
// the default logger, log.Print writes JSON.
func TestNoDeadlockWithLogPrint(t *testing.T) {
	var mu sync.Mutex
	var out bytes.Buffer
	w := writerFunc(func(p []byte) (int, error) {
		mu.Lock()
		defer mu.Unlock()
		return out.Write(p)
	})
	withDefaultLog(t, w, log.LstdFlags)
	s := New(Options{})
	done := make(chan struct{})
	go func() {
		var wg sync.WaitGroup
		for i := range 8 {
			wg.Go(func() {
				for j := range 200 {
					if i%2 == 0 {
						log.Println("print", j)
					} else {
						s.Logger(World).Info("line", "j", j)
					}
				}
			})
		}
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("lines of a Set and log.Print did not end")
	}
	mu.Lock()
	if n := strings.Count(out.String(), "\n"); n != 1600 {
		t.Errorf("%d lines written, want 1600", n)
	}
	mu.Unlock()

	var js bytes.Buffer
	old := slog.Default()
	t.Cleanup(func() { slog.SetDefault(old) })
	s = New(Options{Format: JSON, Output: &js})
	slog.SetDefault(s.Logger("default"))
	// log.Print writes records of the level of the bridge of log.
	slog.SetLogLoggerLevel(slog.LevelInfo)
	log.Print("pprof and metrics")
	var v map[string]any
	if err := json.Unmarshal(js.Bytes(), &v); err != nil || v["msg"] != "pprof and metrics" || v["component"] != "default" {
		t.Errorf("log.Print with a Set of JSON the default: %q, %v", js.String(), err)
	}
}

// writerFunc is an io.Writer of a function.
type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

func BenchmarkHandlerDisabled(b *testing.B) {
	l := New(Options{Logger: log.New(io.Discard, "", 0)}).Logger(Session)
	b.ReportAllocs()
	for b.Loop() {
		l.Debug("process packet", "name", "Steve")
	}
}

func BenchmarkHandlerText(b *testing.B) {
	l := New(Options{Logger: log.New(io.Discard, "", log.LstdFlags)}).Logger(Session).With("name", "Steve")
	b.ReportAllocs()
	for b.Loop() {
		l.Info("line", "a", 1, "b", "two", "c", 1.5)
	}
}

func BenchmarkHandlerJSON(b *testing.B) {
	l := New(Options{Format: JSON, Output: io.Discard}).Logger(Session).With("name", "Steve")
	b.ReportAllocs()
	for b.Loop() {
		l.Info("line", "a", 1, "b", "two", "c", 1.5)
	}
}

func BenchmarkDefaultHandler(b *testing.B) {
	out, f := log.Writer(), log.Flags()
	log.SetOutput(io.Discard)
	log.SetFlags(log.LstdFlags)
	defer func() { log.SetOutput(out); log.SetFlags(f) }()
	l := slog.Default().With("name", "Steve")
	b.ReportAllocs()
	for b.Loop() {
		l.Info("line", "a", 1, "b", "two", "c", 1.5)
	}
}
