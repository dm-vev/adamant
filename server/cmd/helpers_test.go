package cmd

import (
	"reflect"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/go-gl/mathgl/mgl64"
)

// testSource is a command source at a position with a rotation that records
// the output sent back to it.
type testSource struct {
	pos mgl64.Vec3
	rot cube.Rotation
	out *Output
}

func (s testSource) Position() mgl64.Vec3    { return s.pos }
func (s testSource) Rotation() cube.Rotation { return s.rot }
func (s testSource) SendCommandOutput(o *Output) {
	if s.out != nil {
		*s.out = *o
	}
}

// parseOne parses args into a value of type T, as a command field of that
// type would be, and returns the value and the error.
func parseOne[T any](t *testing.T, args ...string) (T, error) {
	t.Helper()
	var v T
	line := &Line{args: args, src: testSource{}, seen: []string{"/test"}, cmd: Command{usage: "/test"}}
	err, _ := parser{}.parseArgument(line, reflect.ValueOf(&v).Elem(), false, "v", testSource{}, nil)
	if err == nil && line.Len() != 0 {
		t.Fatalf("parsing %q left %q on the line", args, line.args)
	}
	return v, err
}
