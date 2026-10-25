package builtin

import (
	"reflect"
	"strconv"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// commandAngle is a yaw or pitch argument, absolute or relative (~) to the
// rotation of the command source, as Java's RotationArgument.
type commandAngle struct {
	Value    float64
	Relative bool
}

// Type ...
func (commandAngle) Type() string { return "value" }

// Parse ...
func (commandAngle) Parse(line *cmd.Line, v reflect.Value) error {
	arg, ok := line.Next()
	if !ok {
		return line.UsageError()
	}
	a := commandAngle{}
	if rest, relative := strings.CutPrefix(arg, "~"); relative {
		a.Relative, arg = true, rest
		if arg == "" {
			v.Set(reflect.ValueOf(a))
			return nil
		}
	}
	value, err := strconv.ParseFloat(arg, 64)
	if err != nil {
		return cmd.MessageNumberInvalid.F(arg)
	}
	a.Value = value
	v.Set(reflect.ValueOf(a))
	return nil
}

// of returns the angle relative to the rotation of the source.
func (a commandAngle) of(source float64) float64 {
	if a.Relative {
		return source + a.Value
	}
	return a.Value
}

type entityAnchor string

func (entityAnchor) Type() string                { return "EntityAnchor" }
func (entityAnchor) Options(cmd.Source) []string { return []string{"eyes", "feet"} }

// rotateCommand ports RotateCommand: /rotate <target> <yRot> <xRot>.
type rotateCommand struct {
	worldOpCommand
	Target []cmd.Target `cmd:"target"`
	Yaw    commandAngle `cmd:"yRot"`
	Pitch  commandAngle `cmd:"xRot"`
}

// rotateFacingCommand is /rotate <target> facing <facingLocation>.
type rotateFacingCommand struct {
	worldOpCommand
	Target   []cmd.Target   `cmd:"target"`
	Facing   cmd.SubCommand `cmd:"facing"`
	Location mgl64.Vec3     `cmd:"facingLocation"`
}

// rotateFacingEntityCommand is /rotate <target> facing entity <facingEntity>
// [<facingAnchor>].
type rotateFacingEntityCommand struct {
	worldOpCommand
	Target       []cmd.Target               `cmd:"target"`
	Facing       cmd.SubCommand             `cmd:"facing"`
	Entity       cmd.SubCommand             `cmd:"entity"`
	FacingEntity []cmd.Target               `cmd:"facingEntity"`
	Anchor       cmd.Optional[entityAnchor] `cmd:"facingAnchor"`
}

func registerRotateCommand(guard worldOpCommand) {
	cmd.Register(newCommand("rotate", nil, rotateCommand{worldOpCommand: guard}, rotateFacingCommand{worldOpCommand: guard}, rotateFacingEntityCommand{worldOpCommand: guard}))
}

// Run ...
func (c rotateCommand) Run(src cmd.Source, o *cmd.Output, _ *world.Tx) {
	target, ok := singleTarget(c.Target, o)
	if !ok {
		return
	}
	source := sourceRotation(src)
	setTargetRotation(target, c.Yaw.of(source.Yaw()), c.Pitch.of(source.Pitch()))
	o.Printf("Rotated %s", commandTargetName(target))
}

// Run ...
func (c rotateFacingCommand) Run(src cmd.Source, o *cmd.Output, _ *world.Tx) {
	target, ok := singleTarget(c.Target, o)
	if !ok {
		return
	}
	lookAtFrom(target, c.Location, src)
	o.Printf("Rotated %s", commandTargetName(target))
}

// Run ...
func (c rotateFacingEntityCommand) Run(src cmd.Source, o *cmd.Output, _ *world.Tx) {
	target, ok := singleTarget(c.Target, o)
	if !ok {
		return
	}
	facing, ok := singleTarget(c.FacingEntity, o)
	if !ok {
		return
	}
	look := facing.Position()
	if c.Anchor.LoadOr("feet") == "eyes" {
		if eyed, ok := facing.(interface{ EyeHeight() float64 }); ok {
			look[1] += eyed.EyeHeight()
		}
	}
	lookAtFrom(target, look, src)
	o.Printf("Rotated %s", commandTargetName(target))
}

// lookAtFrom ports Entity.lookAt: the target turns to look at the position
// from its feet, or its eyes if the command runs anchored at the eyes.
func lookAtFrom(target cmd.Target, look mgl64.Vec3, src cmd.Source) {
	from := target.Position()
	if context, ok := cmd.ExecutionContextOf(src); ok && context.Anchor == cmd.Eyes {
		if eyed, ok := target.(interface{ EyeHeight() float64 }); ok {
			from[1] += eyed.EyeHeight()
		}
	}
	yaw, pitch := teleportLookAt(from, look)
	setTargetRotation(target, yaw, pitch)
}

// setTargetRotation turns a target to an absolute yaw and pitch.
func setTargetRotation(target cmd.Target, yaw, pitch float64) {
	moveTeleportTarget(target, target.Position(), yaw, min(max(pitch, -90), 90))
}

// sourceRotation returns the rotation a command runs with.
func sourceRotation(src cmd.Source) cube.Rotation {
	if context, ok := cmd.ExecutionContextOf(src); ok {
		return context.Rotation
	}
	if rotated, ok := src.(interface{ Rotation() cube.Rotation }); ok {
		return rotated.Rotation()
	}
	return cube.Rotation{}
}
