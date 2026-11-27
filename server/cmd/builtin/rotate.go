package builtin

import (
	"reflect"
	"strings"

	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/cmd"
	"github.com/dm-vev/adamant/server/world"
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
	value, err := cmd.ParseNumber(arg, 64)
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
	Target cmd.OneEntity `cmd:"target"`
	Yaw    commandAngle  `cmd:"yRot"`
	Pitch  commandAngle  `cmd:"xRot"`
}

// rotateFacingCommand is /rotate <target> facing <facingLocation>.
type rotateFacingCommand struct {
	worldOpCommand
	Target   cmd.OneEntity  `cmd:"target"`
	Facing   cmd.SubCommand `cmd:"facing"`
	Location mgl64.Vec3     `cmd:"facingLocation"`
}

// rotateFacingEntityCommand is /rotate <target> facing entity <facingEntity>
// [<facingAnchor>].
type rotateFacingEntityCommand struct {
	worldOpCommand
	Target       cmd.OneEntity              `cmd:"target"`
	Facing       cmd.SubCommand             `cmd:"facing"`
	Entity       cmd.SubCommand             `cmd:"entity"`
	FacingEntity cmd.OneEntity              `cmd:"facingEntity"`
	Anchor       cmd.Optional[entityAnchor] `cmd:"facingAnchor"`
}

func registerRotateCommand(guard worldOpCommand) {
	cmd.Register(newCommand("rotate", nil, rotateCommand{worldOpCommand: guard}, rotateFacingCommand{worldOpCommand: guard}, rotateFacingEntityCommand{worldOpCommand: guard}))
}

// Run ...
func (c rotateCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	source := sourceRotation(src)
	yaw, pitch := c.Yaw.of(source.Yaw()), c.Pitch.of(source.Pitch())
	cmd.ForEach(src, o, tx, c.Target,
		func(_ *world.Tx, target cmd.Target) string {
			setTargetRotation(target, yaw, pitch)
			return commandTargetName(target)
		},
		func(_ cmd.Source, o *cmd.Output, _ *world.Tx, rs []cmd.Result[string]) {
			if len(rs) == 0 {
				o.Error(cmd.ErrNoEntityFound)
				return
			}
			o.SetResult(1)
			o.Printf("Rotated %s", rs[0].Value)
		},
	)
}

// Run ...
func (c rotateFacingCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	loc := c.Location
	anchorEyes := anchoredAtEyes(src)
	cmd.ForEach(src, o, tx, c.Target,
		func(_ *world.Tx, target cmd.Target) string {
			lookAtFromAnchor(target, loc, anchorEyes)
			return commandTargetName(target)
		},
		func(_ cmd.Source, o *cmd.Output, _ *world.Tx, rs []cmd.Result[string]) {
			if len(rs) == 0 {
				o.Error(cmd.ErrNoEntityFound)
				return
			}
			o.SetResult(1)
			o.Printf("Rotated %s", rs[0].Value)
		},
	)
}

// Run ...
func (c rotateFacingEntityCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	facingEyes := c.Anchor.LoadOr("feet") == "eyes"
	anchorEyes := anchoredAtEyes(src)
	targetSel := c.Target
	cmd.ForEach(src, o, tx, c.FacingEntity,
		func(_ *world.Tx, facing cmd.Target) mgl64.Vec3 {
			look := facing.Position()
			if facingEyes {
				if eyed, ok := facing.(interface{ EyeHeight() float64 }); ok {
					look[1] += eyed.EyeHeight()
				}
			}
			return look
		},
		func(src cmd.Source, o *cmd.Output, tx *world.Tx, rs []cmd.Result[mgl64.Vec3]) {
			if len(rs) == 0 {
				o.Error(cmd.ErrNoEntityFound)
				return
			}
			look := rs[0].Value
			cmd.ForEach(src, o, tx, targetSel,
				func(_ *world.Tx, target cmd.Target) string {
					lookAtFromAnchor(target, look, anchorEyes)
					return commandTargetName(target)
				},
				func(_ cmd.Source, o *cmd.Output, _ *world.Tx, trs []cmd.Result[string]) {
					if len(trs) == 0 {
						o.Error(cmd.ErrNoEntityFound)
						return
					}
					o.SetResult(1)
					o.Printf("Rotated %s", trs[0].Value)
				},
			)
		},
	)
}

func anchoredAtEyes(src cmd.Source) bool {
	context, ok := cmd.ExecutionContextOf(src)
	return ok && context.Anchor == cmd.Eyes
}

// lookAtFromAnchor ports Entity.lookAt: the target turns to look at the
// position from its feet, or its eyes if anchoredAtEyes is true.
func lookAtFromAnchor(target cmd.Target, look mgl64.Vec3, eyes bool) {
	from := target.Position()
	if eyes {
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
