package builtin

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// placeTemplateCommand ports PlaceCommand.placeTemplate: /place template
// places a structure template, as a structure block in load mode would, but
// mirrored and rotated around its position as Java does.
type placeTemplateCommand struct {
	worldOpCommand
	Template  cmd.SubCommand                     `cmd:"template"`
	Name      string                             `cmd:"id"`
	Position  cmd.Optional[mgl64.Vec3]           `cmd:"pos"`
	Rotation  cmd.Optional[templateRotation]     `cmd:"rotation"`
	Mirror    cmd.Optional[templateMirror]       `cmd:"mirror"`
	Integrity cmd.Optional[float64]              `cmd:"integrity"`
	Seed      cmd.Optional[int]                  `cmd:"seed"`
	Strict    cmd.Optional[templateStrictOption] `cmd:"strict"`
}

type templateRotation string

func (templateRotation) Type() string { return "TemplateRotation" }
func (templateRotation) Options(cmd.Source) []string {
	return []string{"none", "clockwise_90", "180", "counterclockwise_90"}
}

type templateMirror string

func (templateMirror) Type() string { return "TemplateMirror" }
func (templateMirror) Options(cmd.Source) []string {
	return []string{"none", "left_right", "front_back"}
}

type templateStrictOption string

func (templateStrictOption) Type() string                { return "Strict" }
func (templateStrictOption) Options(cmd.Source) []string { return []string{"strict"} }

func registerPlaceCommand(guard worldOpCommand) {
	cmd.Register(newCommand("place", nil, placeTemplateCommand{worldOpCommand: guard}))
}

// Run ...
func (c placeTemplateCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	pos := cube.PosFromVec3(src.Position())
	if v, ok := c.Position.Load(); ok {
		pos = cube.PosFromVec3(v)
	}
	integrity := c.Integrity.LoadOr(1)
	if integrity < 0 || integrity > 1 {
		o.Errorf("Float must be between 0 and 1, found %v", integrity)
		return
	}
	template, ok, err := tx.World().StructureTemplate(c.Name)
	if err != nil || !ok {
		o.Errorf("There is no template with id \"%s\"", c.Name)
		return
	}
	size := template.Size()
	low, high := world.ChunkPos{int32(pos[0] >> 4), int32(pos[2] >> 4)}, world.ChunkPos{int32((pos[0] + size[0]) >> 4), int32((pos[2] + size[2]) >> 4)}
	for x := low[0]; x <= high[0]; x++ {
		for z := low[1]; z <= high[1]; z++ {
			if !tx.ChunkLoaded(world.ChunkPos{x, z}) {
				o.Error("That position is not loaded")
				return
			}
		}
	}
	placement := world.StructurePlacement{Integrity: integrity, Seed: int64(c.Seed.LoadOr(0)), KnownShape: c.Strict.LoadOr("") == "strict"}
	switch c.Rotation.LoadOr("none") {
	case "clockwise_90":
		placement.Rotation = world.StructureRotationClockwise90
	case "180":
		placement.Rotation = world.StructureRotationClockwise180
	case "counterclockwise_90":
		placement.Rotation = world.StructureRotationCounterClockwise90
	}
	switch c.Mirror.LoadOr("none") {
	case "left_right":
		placement.Mirror = world.StructureMirrorLeftRight
	case "front_back":
		placement.Mirror = world.StructureMirrorFrontBack
	}
	if !tx.PlaceStructure(template, pos, placement) {
		o.Error("Failed to place template")
		return
	}
	o.Printf("Loaded template \"%s\" at %d, %d, %d", c.Name, pos[0], pos[1], pos[2])
}
