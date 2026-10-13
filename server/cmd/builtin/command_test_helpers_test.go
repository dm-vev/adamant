package builtin

import (
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
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
