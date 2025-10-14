package plugin

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
	"iter"
	"log/slog"
)

type testServer struct{}
type testConfig struct{}

type testHost struct{}

func (testHost) Instance() testServer { return testServer{} }
func (testHost) Config() testConfig   { return testConfig{} }
func (testHost) Logger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}))
}
func (testHost) StartTime() time.Time { return time.Time{} }
func (testHost) Listen()              {}
func (testHost) Accept() iter.Seq[*player.Player] {
	return func(func(*player.Player) bool) {}
}
func (testHost) World() *world.World  { return nil }
func (testHost) Nether() *world.World { return nil }
func (testHost) End() *world.World    { return nil }
func (testHost) MaxPlayerCount() int  { return 0 }
func (testHost) PlayerCount() int     { return 0 }
func (testHost) Players(*world.Tx) iter.Seq[*player.Player] {
	return func(func(*player.Player) bool) {}
}
func (testHost) Player(uuid.UUID) (*world.EntityHandle, bool)    { return nil, false }
func (testHost) PlayerByName(string) (*world.EntityHandle, bool) { return nil, false }
func (testHost) PlayerByXUID(string) (*world.EntityHandle, bool) { return nil, false }
func (testHost) ExecuteCommand(cmd.Source, string)               {}
func (testHost) PlayerSummaries() []PlayerSummary                { return nil }
func (testHost) CloseOnProgramEnd()                              {}
func (testHost) Close() error                                    { return nil }
func (testHost) LoadPlugins()                                    {}
func (testHost) PluginsEnabled() bool                            { return true }

func TestSanitizePluginDirectory(t *testing.T) {
	cases := map[string]string{
		"":                  "plugin",
		"   ":               "plugin",
		"Example Plugin":    "example-plugin",
		"Example_Plugin":    "example_plugin",
		"Example.Plugin":    "example.plugin",
		"Example@Plugin#":   "example-plugin",
		"--Already-Safe--":  "already-safe",
		"MiXeD CaSe Name":   "mixed-case-name",
		"    dots...here  ": "dots...here",
	}

	for input, want := range cases {
		if got := sanitizePluginDirectory(input); got != want {
			t.Fatalf("sanitizePluginDirectory(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestPluginBaseName(t *testing.T) {
	cases := map[string]string{
		"":                  "plugin",
		"file":              "file",
		"file.so":           "file",
		"path/to/plugin":    "plugin",
		"path/to/plugin.so": "plugin",
		"path/.hidden.so":   ".hidden",
	}

	for input, want := range cases {
		if got := pluginBaseName(input); got != want {
			t.Fatalf("pluginBaseName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestManagerPluginDataDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	manager := NewManager[testServer, testConfig](testHost{}, Config{Enabled: true, Directory: root})

	got := manager.pluginDataDirectory("Example Plugin")
	want := filepath.Join(root, "data", "example-plugin")
	if got != want {
		t.Fatalf("pluginDataDirectory returned %q, want %q", got, want)
	}

	// Ensure directories nested inside the configured data root are resolved correctly when overridden.
	manager.cfg.DataDirectory = "custom"
	got = manager.pluginDataDirectory("Another Plugin")
	want = filepath.Join(root, "custom", "another-plugin")
	if got != want {
		t.Fatalf("pluginDataDirectory with custom root returned %q, want %q", got, want)
	}
}

func TestManagerMigrateDataDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	manager := NewManager[testServer, testConfig](testHost{}, Config{Enabled: true, Directory: root})

	from := filepath.Join(root, "old")
	to := filepath.Join(root, "new")
	if err := os.MkdirAll(from, 0o755); err != nil {
		t.Fatalf("create source directory: %v", err)
	}
	payload := []byte("payload")
	if err := os.WriteFile(filepath.Join(from, "data.txt"), payload, 0o644); err != nil {
		t.Fatalf("write source data: %v", err)
	}

	if err := manager.migrateDataDirectory(from, to); err != nil {
		t.Fatalf("migrate data directory: %v", err)
	}

	if _, err := os.Stat(to); err != nil {
		t.Fatalf("target directory missing: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(to, "data.txt"))
	if err != nil {
		t.Fatalf("read migrated file: %v", err)
	}
	if string(data) != string(payload) {
		t.Fatalf("migrated data mismatch: got %q, want %q", string(data), string(payload))
	}
	if _, err := os.Stat(from); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("source directory still exists after migrate")
	}
}

func TestManagerMigrateDataDirectoryCreatesTarget(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	manager := NewManager[testServer, testConfig](testHost{}, Config{Enabled: true, Directory: root})

	target := filepath.Join(root, "generated")
	if err := manager.migrateDataDirectory("", target); err != nil {
		t.Fatalf("migrateDataDirectory should create target when source empty: %v", err)
	}
	if info, err := os.Stat(target); err != nil {
		t.Fatalf("stat generated target: %v", err)
	} else if !info.IsDir() {
		t.Fatalf("generated target is not a directory")
	}
}

func TestManagerDirectoryResolution(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	manager := NewManager[testServer, testConfig](testHost{}, Config{Enabled: true, Directory: root, DataDirectory: "state"})

	if got, want := manager.Directory(), root; got != want {
		t.Fatalf("Directory() = %q, want %q", got, want)
	}
	if got, want := manager.DataRoot(), filepath.Join(root, "state"); got != want {
		t.Fatalf("DataRoot() = %q, want %q", got, want)
	}
	rel := manager.ResolvePath("example.so")
	if want := filepath.Join(root, "example.so"); rel != want {
		t.Fatalf("ResolvePath relative = %q, want %q", rel, want)
	}
	abs := filepath.Join(root, "other.so")
	if got := manager.ResolvePath(abs); got != abs {
		t.Fatalf("ResolvePath absolute = %q, want %q", got, abs)
	}
}

type closingPlugin struct {
	name   string
	closed chan struct{}
}

func (p *closingPlugin) Name() string { return p.name }

func (p *closingPlugin) Close() error {
	select {
	case <-p.closed:
	default:
		close(p.closed)
	}
	return nil
}

func TestManagerDisableAll(t *testing.T) {
	t.Parallel()

	manager := NewManager[testServer, testConfig](testHost{}, Config{Enabled: true})
	manager.cfg.Enabled = true

	first := &closingPlugin{name: "first", closed: make(chan struct{})}
	second := &closingPlugin{name: "second", closed: make(chan struct{})}

	manager.plugins = []pluginInstance[testServer, testConfig]{
		{name: first.name, plugin: first, path: "first.so"},
		{name: second.name, plugin: second, path: "second.so"},
	}

	infos, err := manager.DisableAll()
	if err != nil {
		t.Fatalf("DisableAll() error = %v", err)
	}
	if len(infos) != 2 {
		t.Fatalf("DisableAll() returned %d infos, want 2", len(infos))
	}
	if infos[0].Name != "second" || infos[1].Name != "first" {
		t.Fatalf("DisableAll() order = %v", infos)
	}

	select {
	case <-first.closed:
	default:
		t.Fatalf("first plugin was not closed")
	}
	select {
	case <-second.closed:
	default:
		t.Fatalf("second plugin was not closed")
	}

	if got := manager.Infos(); len(got) != 0 {
		t.Fatalf("DisableAll() left %d plugins loaded", len(got))
	}
}

func TestManagerDisableAllDisabled(t *testing.T) {
	t.Parallel()

	manager := NewManager[testServer, testConfig](testHost{}, Config{Enabled: false})

	if infos, err := manager.DisableAll(); !errors.Is(err, ErrDisabled) || infos != nil {
		t.Fatalf("DisableAll() = (%v, %v), want (nil, ErrDisabled)", infos, err)
	}
}

func TestManagerHandlePluginPanicDisablesPlugin(t *testing.T) {
	t.Parallel()

	manager := NewManager[testServer, testConfig](testHost{}, Config{Enabled: true})
	manager.cfg.Enabled = true

	closed := make(chan struct{})
	plugin := &closingPlugin{name: "panic", closed: closed}
	manager.plugins = []pluginInstance[testServer, testConfig]{
		{
			name:   "panic",
			plugin: plugin,
			path:   "panic.so",
		},
	}
	manager.events.addPlayer("panic", player.NopHandler{})

	manager.handlePluginPanic("panic", errors.New("boom"))

	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatalf("plugin close was not invoked after panic")
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		manager.mu.RLock()
		remaining := len(manager.plugins)
		manager.mu.RUnlock()
		if remaining == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("plugin was not removed after panic")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if regs := manager.events.loadPlayerChain(); len(regs) != 0 {
		t.Fatalf("expected player handlers to be cleared, got %d registrations", len(regs))
	}
}

// Ensure compile-time conformance for the test host.
var _ Host[testServer, testConfig] = testHost{}
