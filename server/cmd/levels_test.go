package cmd

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// levelSource is a source of a level of its own, as a player is.
type levelSource struct{ level Level }

func (levelSource) Position() mgl64.Vec3         { return mgl64.Vec3{} }
func (levelSource) SendCommandOutput(*Output)    {}
func (s levelSource) PermissionLevel() Level     { return s.level }
func (embedderSource) Position() mgl64.Vec3      { return mgl64.Vec3{} }
func (embedderSource) SendCommandOutput(*Output) {}

// embedderSource is a source of the code that embeds the server: no level
// of its own.
type embedderSource struct{}

// testPermissions overrides the level of the commands of its map, and runs
// command blocks at block.
type testPermissions struct {
	block     Level
	overrides map[string]Level
}

func (testPermissions) PlayerLevel(uuid.UUID) Level { return LevelModerator }
func (p testPermissions) CommandLevel(name string, declared Level) Level {
	if l, ok := p.overrides[name]; ok {
		return l
	}
	return declared
}
func (p testPermissions) CommandBlockLevel() Level { return p.block }

// levelRunnable is a runnable that declares a level.
type levelRunnable struct{ level Level }

func (r levelRunnable) RequiredLevel() Level         { return r.level }
func (levelRunnable) Run(Source, *Output, *world.Tx) {}

// TestLevelsOfSources checks the level of each kind of source: a Leveled
// source its own, also wrapped by /execute; a command block the level the
// permissions give command blocks, LevelGameMaster until any are
// installed; a source with no level of its own LevelOwner.
func TestLevelsOfSources(t *testing.T) {
	t.Cleanup(func() { SetPermissions(nil) })
	SetPermissions(nil)
	block := NewBlockSource(cube.Pos{}, nil, "")
	for _, tc := range []struct {
		name string
		src  Source
		want Level
	}{
		{"a source of level 1", levelSource{LevelModerator}, LevelModerator},
		{"execute by it", WithContext(levelSource{LevelModerator}, ExecutionContext{}), LevelModerator},
		{"execute in execute by it", WithContext(WithContext(levelSource{LevelAdmin}, ExecutionContext{}), ExecutionContext{}), LevelAdmin},
		{"a command block", block, LevelGameMaster},
		{"execute by a command block", WithContext(block, ExecutionContext{}), LevelGameMaster},
		{"an embedder's source", embedderSource{}, LevelOwner},
	} {
		if got := SourceLevel(tc.src); got != tc.want {
			t.Errorf("%s: level %v, want %v", tc.name, got, tc.want)
		}
	}
	if got := PlayerLevel(uuid.New()); got != LevelAll {
		t.Errorf("a player with no permissions installed: level %v, want 0", got)
	}

	SetPermissions(testPermissions{block: LevelAdmin})
	if got := SourceLevel(block); got != LevelAdmin {
		t.Errorf("a command block of permissions at level 3: level %v", got)
	}
	if got := PlayerLevel(uuid.New()); got != LevelModerator {
		t.Errorf("a player: level %v, want the permissions' 1", got)
	}
}

// TestCommandLevelOverridesAndUnknown checks that a source below the level
// of a runnable may not run it, that the permissions override the level
// by the name of the command, and that a denied source is told the
// command does not exist.
func TestCommandLevelOverridesAndUnknown(t *testing.T) {
	t.Cleanup(func() { SetPermissions(nil) })
	SetPermissions(nil)
	c := New("kickish", "", nil, levelRunnable{LevelAdmin})
	if n := len(c.Runnables(levelSource{LevelGameMaster})); n != 0 {
		t.Errorf("a game master may run %d runnables of a level-3 command", n)
	}
	if n := len(c.Runnables(levelSource{LevelAdmin})); n != 1 {
		t.Errorf("an admin may run %d runnables of a level-3 command", n)
	}
	if n := len(c.Params(levelSource{LevelGameMaster})); n != 0 {
		t.Errorf("a game master is shown %d overloads of a level-3 command", n)
	}
	SetPermissions(testPermissions{overrides: map[string]Level{"kickish": LevelGameMaster}})
	if n := len(c.Runnables(levelSource{LevelGameMaster})); n != 1 {
		t.Errorf("a game master may run %d runnables of a command overridden to level 2", n)
	}
	SetPermissions(testPermissions{overrides: map[string]Level{"kickish": LevelOwner}})
	var out Output
	c.Execute("", outputSource{levelSource{LevelAdmin}, &out}, nil)
	if errs := out.Errors(); len(errs) != 1 || errs[0].Error() != MessageUnknown.F("kickish").Error() {
		t.Errorf("an admin running a command overridden to level 4: %v", errs)
	}
}

// outputSource is a Leveled source that keeps the output it is sent.
type outputSource struct {
	levelSource
	out *Output
}

func (s outputSource) SendCommandOutput(o *Output) { *s.out = *o }
