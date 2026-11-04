package server_test

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/cmd/builtin"
	"github.com/go-gl/mathgl/mgl64"
)

// permissionServer returns a server keyed by name whose lists are in dir,
// with ops.json holding ops if it is not empty.
func permissionServer(t *testing.T, dir, ops string) *server.Server {
	t.Helper()
	if ops != "" {
		if err := os.WriteFile(filepath.Join(dir, "ops.json"), []byte(ops), 0644); err != nil {
			t.Fatal(err)
		}
	}
	uc := server.DefaultConfig()
	uc.World.SaveData, uc.Players.SaveData = false, false
	uc.Whitelist.File = filepath.Join(dir, "white-list.txt")
	conf, err := uc.Config(slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	conf.Log = slog.New(slog.DiscardHandler)
	srv := conf.New()
	srv.Listen()
	t.Cleanup(func() { _ = srv.Close() })
	return srv
}

// TestOpsLevelsRoundTrip checks the levels of ops.json: an entry without
// one, of a name or an account, has the level /op gives; a file of such
// entries writes back byte for byte; SetOperatorLevel writes "level", an
// explicit 0 included, and bypassesPlayerLimit reads and writes back.
func TestOpsLevelsRoundTrip(t *testing.T) {
	const old = "[\n  \"alex\",\n  {\n    \"xuid\": \"2535412345678901\",\n    \"name\": \"Steve\"\n  }\n]\n"
	dir := t.TempDir()
	srv := permissionServer(t, dir, old)
	if l, ok := srv.OperatorLevel("alex"); !ok || l != server.DefaultOperatorLevel {
		t.Errorf("alex of an old file: level %v, %v, want %v", l, ok, server.DefaultOperatorLevel)
	}
	if l, ok := srv.OperatorLevel("xuid:2535412345678901"); !ok || l != server.DefaultOperatorLevel {
		t.Errorf("the account of an old file: level %v, %v", l, ok)
	}
	if _, ok := srv.OperatorLevel("nobody"); ok {
		t.Error("nobody is an operator")
	}
	// Adding and removing an entry rewrites the file as it was.
	if _, err := srv.Op("temp"); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Deop("temp"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(dir, "ops.json")); got != old {
		t.Errorf("old ops.json rewritten as\n%s\nwant\n%s", got, old)
	}

	for _, s := range []struct {
		name  string
		level cmd.Level
	}{{"alex", cmd.LevelAdmin}, {"builder", cmd.LevelAll}} {
		if changed, err := srv.SetOperatorLevel(s.name, s.level); err != nil || !changed {
			t.Fatalf("SetOperatorLevel(%q, %v) = %v, %v", s.name, s.level, changed, err)
		}
		if l, ok := srv.OperatorLevel(s.name); !ok || l != s.level {
			t.Errorf("%s: level %v, %v, want %v", s.name, l, ok, s.level)
		}
	}
	if changed, _ := srv.SetOperatorLevel("alex", cmd.LevelAdmin); changed {
		t.Error("setting the level an operator has changed the list")
	}
	const leveled = "[\n  {\n    \"name\": \"alex\",\n    \"level\": 3\n  },\n  {\n    \"xuid\": \"2535412345678901\",\n    \"name\": \"Steve\"\n  },\n  {\n    \"name\": \"builder\",\n    \"level\": 0\n  }\n]\n"
	if got := readFile(t, filepath.Join(dir, "ops.json")); got != leveled {
		t.Errorf("ops.json with levels:\n%s\nwant\n%s", got, leveled)
	}

	// A file of Java's fields reads, and writes back as it was.
	const java = "[\n  {\n    \"name\": \"mod\",\n    \"level\": 2,\n    \"bypassesPlayerLimit\": true\n  },\n  {\n    \"xuid\": \"2535498765432109\",\n    \"name\": \"Admin\",\n    \"level\": 3\n  }\n]\n"
	dir = t.TempDir()
	srv = permissionServer(t, dir, java)
	if l, ok := srv.OperatorLevel("mod"); !ok || l != cmd.LevelGameMaster {
		t.Errorf("mod: level %v, %v, want 2", l, ok)
	}
	if _, err := srv.Op("temp"); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Deop("temp"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(dir, "ops.json")); got != java {
		t.Errorf("ops.json of Java's fields rewritten as\n%s\nwant\n%s", got, java)
	}

	// A level out of range is an error of the file.
	dir = t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ops.json"), []byte(`[{"name":"x","level":5}]`), 0644); err != nil {
		t.Fatal(err)
	}
	uc := server.DefaultConfig()
	uc.World.SaveData, uc.Players.SaveData = false, false
	uc.Whitelist.File = filepath.Join(dir, "white-list.txt")
	if _, err := uc.Config(slog.New(slog.DiscardHandler)); err == nil || !strings.Contains(err.Error(), "level 5: want 0 to 4") {
		t.Errorf("ops.json of level 5: error %v", err)
	}
}

// readFile returns the contents of the file at path.
func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// levelSource is a source of a level of its own that keeps the output it
// is sent.
type levelSource struct {
	level cmd.Level
	out   *cmd.Output
}

func (levelSource) Position() mgl64.Vec3              { return mgl64.Vec3{} }
func (s levelSource) PermissionLevel() cmd.Level      { return s.level }
func (s levelSource) SendCommandOutput(o *cmd.Output) { *s.out = *o }

// run runs the command line passed as src and returns its output.
func run(t *testing.T, level cmd.Level, line string) *cmd.Output {
	t.Helper()
	out := &cmd.Output{}
	cmd.ExecuteLine(levelSource{level: level, out: out}, line, nil, nil)
	return out
}

// TestOpCannotGrantAboveOwnLevel checks that no source gives or takes a
// level above its own: an admin of level 3 neither makes an operator of
// level 4 nor removes one, while the console, of level 4, does both.
func TestOpCannotGrantAboveOwnLevel(t *testing.T) {
	srv := permissionServer(t, t.TempDir(), "")
	builtin.Register(srv)
	t.Cleanup(func() { cmd.SetPermissions(nil) })
	if out := run(t, cmd.LevelAdmin, "/op Steve"); len(out.Errors()) != 1 || out.Errors()[0].Error() != "You can't give a level above your own (3)." {
		t.Errorf("/op from level 3: %v", out.Errors())
	}
	if srv.IsOperator("Steve") {
		t.Fatal("an admin of level 3 made an operator of level 4")
	}
	if out := run(t, cmd.LevelOwner, "/op Steve"); len(out.Errors()) != 0 || !srv.IsOperator("Steve") {
		t.Fatalf("/op from level 4: %v", out.Errors())
	}
	if out := run(t, cmd.LevelAdmin, "/deop Steve"); len(out.Errors()) != 1 || out.Errors()[0].Error() != "You can't remove an operator above your own level (3)." {
		t.Errorf("/deop of a level-4 operator from level 3: %v", out.Errors())
	}
	if !srv.IsOperator("Steve") {
		t.Fatal("an admin of level 3 removed an operator of level 4")
	}
	if _, err := srv.SetOperatorLevel("Alex", cmd.LevelAdmin); err != nil {
		t.Fatal(err)
	}
	if out := run(t, cmd.LevelAdmin, "/deop Alex"); len(out.Errors()) != 0 || srv.IsOperator("Alex") {
		t.Errorf("/deop of a level-3 operator from level 3: %v", out.Errors())
	}
	if out := run(t, cmd.LevelOwner, "/deop Steve"); len(out.Errors()) != 0 || srv.IsOperator("Steve") {
		t.Errorf("/deop from level 4: %v", out.Errors())
	}
}

// TestCommandLevelOverrides checks [Permissions.Commands]: kick = 2 lets a
// source of level 2 run /kick; teleport = 3, an alias, sets the level of
// /tp; a name no command has is a warning at Listen naming the closest
// command, and with StrictConfig Listen panics of it.
func TestCommandLevelOverrides(t *testing.T) {
	newServer := func(strict bool, log *slog.Logger) *server.Server {
		uc := server.DefaultConfig()
		uc.World.SaveData, uc.Players.SaveData = false, false
		uc.Whitelist.File = filepath.Join(t.TempDir(), "white-list.txt")
		uc.Permissions.Commands = map[string]int{"kick": 2, "teleport": 3, "kik": 1}
		conf, err := uc.Config(slog.New(slog.DiscardHandler))
		if err != nil {
			t.Fatal(err)
		}
		conf.Log, conf.StrictConfig = log, strict
		return conf.New()
	}
	var logged strings.Builder
	srv := newServer(false, slog.New(slog.NewTextHandler(&logged, nil)))
	builtin.Register(srv)
	t.Cleanup(func() { cmd.SetPermissions(nil) })
	srv.Listen()
	t.Cleanup(func() { _ = srv.Close() })
	if want := "config.toml: Permissions.Commands.kik: no command is named kik, did you mean kick?"; !strings.Contains(logged.String(), want) {
		t.Errorf("logged\n%s\nwant %q", logged.String(), want)
	}
	if l := srv.CommandLevel("kick", cmd.LevelAdmin); l != cmd.LevelGameMaster {
		t.Errorf("/kick needs level %v, want 2", l)
	}
	if l := srv.CommandLevel("tp", cmd.LevelGameMaster); l != cmd.LevelAdmin {
		t.Errorf("/tp, set as teleport, needs level %v, want 3", l)
	}
	kick, _ := cmd.ByAlias("kick")
	if len(kick.Runnables(levelSource{level: cmd.LevelGameMaster, out: &cmd.Output{}})) == 0 {
		t.Error("a source of level 2 may not run /kick of level 2")
	}
	tp, _ := cmd.ByAlias("teleport")
	if len(tp.Runnables(levelSource{level: cmd.LevelGameMaster, out: &cmd.Output{}})) != 0 {
		t.Error("a source of level 2 may run /tp of level 3")
	}

	// The server the panic leaves unstarted cannot be closed.
	strict := newServer(true, slog.New(slog.DiscardHandler))
	defer func() {
		if r := recover(); r == nil || !strings.Contains(fmt.Sprint(r), "Permissions.Commands.kik: no command is named kik") {
			t.Errorf("Listen with StrictConfig: recovered %v", r)
		}
	}()
	strict.Listen()
}
