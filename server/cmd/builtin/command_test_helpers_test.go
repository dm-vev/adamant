package builtin

import (
	"testing"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// consoleTestSource is a trusted non-player source that records the output a
// parsed command sends back, as the server console would.
type consoleTestSource struct {
	pos mgl64.Vec3
	out *cmd.Output
}

func (s consoleTestSource) Position() mgl64.Vec3            { return s.pos }
func (s consoleTestSource) SendCommandOutput(o *cmd.Output) { *s.out = *o }

// runTestCommand parses and executes args through the real command parser.
func runTestCommand(c cmd.Command, args string, tx *world.Tx) *cmd.Output {
	out := &cmd.Output{}
	c.Execute(args, consoleTestSource{out: out}, tx)
	return out
}

// levelTestPermissions are permissions under which every player online
// has the level player, and every command the level it declares.
type levelTestPermissions struct{ player cmd.Level }

func (p levelTestPermissions) PlayerLevel(uuid.UUID) cmd.Level                   { return p.player }
func (levelTestPermissions) CommandLevel(_ string, declared cmd.Level) cmd.Level { return declared }
func (levelTestPermissions) CommandBlockLevel() cmd.Level                        { return cmd.LevelGameMaster }

// playersAtLevel installs permissions under which every player has the
// level passed until the test ends.
func playersAtLevel(t *testing.T, l cmd.Level) {
	t.Helper()
	cmd.SetPermissions(levelTestPermissions{player: l})
	t.Cleanup(func() { cmd.SetPermissions(nil) })
}
