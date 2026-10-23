package world

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
)

func init() {
	registerJavaShape("MangroveRootsBlock", javaPlant06MangroveRootsShape)
	registerJavaShape("MossyCarpetBlock", javaPlant06MossyCarpetShape)
	registerJavaShape("MultifaceBlock", javaPlant06MultifaceShape)
	registerJavaShape("PitcherCropBlock", javaPlant06PitcherCropShape)
	registerJavaShape("SeaPickleBlock", javaPlant06SeaPickleShape)

	registerJavaSurvival("MossyCarpetBlock", javaPlant06MossyCarpetSurvival)
	registerJavaSurvival("MultifaceBlock", javaPlant06MultifaceSurvival)
	registerJavaSurvival("PitcherCropBlock", javaPlant06PitcherCropSurvival)
	registerJavaSurvival("SeaPickleBlock", javaPlant06SeaPickleSurvival)
}

func javaPlant06MangroveRootsShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if javaBool(state, "waterlogged") {
		c.waterTick(state, c.pos)
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func javaPlant06MossyCarpetShape(c *javaShapeContext, state uint32, _ cube.Face, _ uint32) uint32 {
	if !javaPlant06MossyCarpetSurvival(c, state, c.pos) {
		return javaDefault("air")
	}
	updated := javaPlant06UpdateMossyCarpetSides(c, state, false)
	if !javaPlant06MossyCarpetHasFaces(updated) {
		return javaDefault("air")
	}
	return updated
}

func javaPlant06MossyCarpetSurvival(c *javaShapeContext, state uint32, pos cube.Pos) bool {
	below := c.at(pos.Side(cube.FaceDown))
	if javaBool(state, "bottom") {
		return !javaPlant06IsAir(below)
	}
	return javaName(below) == "pale_moss_carpet" && javaBool(below, "bottom")
}

func javaPlant06IsAir(state uint32) bool {
	switch javaName(state) {
	case "air", "cave_air", "void_air":
		return true
	default:
		return false
	}
}

func javaPlant06MossyCarpetHasFaces(state uint32) bool {
	if javaBool(state, "bottom") {
		return true
	}
	for _, property := range [...]string{"north", "east", "south", "west"} {
		if javaString(state, property) != "none" {
			return true
		}
	}
	return false
}

func javaPlant06UpdateMossyCarpetSides(c *javaShapeContext, state uint32, createSides bool) uint32 {
	createSides = createSides || javaBool(state, "bottom")
	var above, below uint32
	aboveRead, belowRead := false, false
	for _, item := range [...]struct {
		face     cube.Face
		property string
	}{{cube.FaceNorth, "north"}, {cube.FaceEast, "east"}, {cube.FaceSouth, "south"}, {cube.FaceWest, "west"}} {
		side := javaString(state, item.property)
		if javaPlant06CanSupportCarpetFace(c, item.face) {
			if createSides {
				side = "low"
			}
		} else {
			side = "none"
		}
		if side == "low" {
			if !aboveRead {
				above = c.at(c.pos.Side(cube.FaceUp))
				aboveRead = true
			}
			if javaName(above) == "pale_moss_carpet" && javaString(above, item.property) != "none" && !javaBool(above, "bottom") {
				side = "tall"
			}
			if !javaBool(state, "bottom") {
				if !belowRead {
					below = c.at(c.pos.Side(cube.FaceDown))
					belowRead = true
				}
				if javaName(below) == "pale_moss_carpet" && javaString(below, item.property) == "none" {
					side = "none"
				}
			}
		}
		state = javaWith(state, item.property, side)
	}
	return state
}

func javaPlant06CanSupportCarpetFace(c *javaShapeContext, direction cube.Face) bool {
	if direction == cube.FaceUp {
		return false
	}
	neighbor := c.at(c.pos.Side(direction))
	return javaPlant06FaceFull(neighbor, direction.Opposite())
}

func javaPlant06MultifaceShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if javaBool(state, "waterlogged") {
		c.waterTick(state, c.pos)
	}
	if !javaPlant06MultifaceHasAnyFace(state) {
		return javaDefault("air")
	}
	if javaBool(state, javaPlant06FaceProperty(face)) && !javaPlant06CanAttachTo(c, c.pos.Side(face), face.Opposite(), neighbor) {
		updated := javaWith(state, javaPlant06FaceProperty(face), false)
		if !javaPlant06MultifaceHasAnyFace(updated) {
			return javaDefault("air")
		}
		return updated
	}
	return state
}

func javaPlant06MultifaceSurvival(c *javaShapeContext, state uint32, pos cube.Pos) bool {
	hasFace := false
	for _, face := range [...]cube.Face{cube.FaceDown, cube.FaceUp, cube.FaceNorth, cube.FaceSouth, cube.FaceWest, cube.FaceEast} {
		if javaBool(state, javaPlant06FaceProperty(face)) {
			hasFace = true
			if !javaPlant06CanAttachTo(c, pos.Side(face), face.Opposite(), c.at(pos.Side(face))) {
				return false
			}
		}
	}
	return hasFace
}

func javaPlant06MultifaceHasAnyFace(state uint32) bool {
	for _, property := range [...]string{"down", "up", "north", "south", "west", "east"} {
		if javaBool(state, property) {
			return true
		}
	}
	return false
}

func javaPlant06CanAttachTo(c *javaShapeContext, _ cube.Pos, supportFace cube.Face, state uint32) bool {
	return javaPlant06FaceFull(state, supportFace)
}

func javaPlant06FaceFull(state uint32, face cube.Face) bool {
	if javaGenerationShape(state).Attachment&(1<<uint(face)) != 0 {
		return true
	}
	full, err := gamedata.GenerationCollisionFaceCoversRectangleID(state, int(face), 0, 0, 1, 1)
	if err != nil {
		panic(err)
	}
	return full
}

func javaPlant06FaceProperty(face cube.Face) string {
	switch face {
	case cube.FaceDown:
		return "down"
	case cube.FaceUp:
		return "up"
	case cube.FaceNorth:
		return "north"
	case cube.FaceSouth:
		return "south"
	case cube.FaceWest:
		return "west"
	case cube.FaceEast:
		return "east"
	default:
		panic("invalid Java multiface direction")
	}
}

func javaPlant06PitcherCropShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if javaInt(state, "age") >= 3 {
		return javaPlant06DoublePlantShape(c, state, face, neighbor)
	}
	if javaPlant06PitcherCropSurvival(c, state, c.pos) {
		return state
	}
	return javaDefault("air")
}

func javaPlant06PitcherCropSurvival(c *javaShapeContext, state uint32, pos cube.Pos) bool {
	lower := javaName(state) == "pitcher_crop" && javaString(state, "half") == "lower"
	if lower && c.light(pos) < 8 {
		return false
	}
	if javaString(state, "half") == "upper" {
		below := c.at(pos.Side(cube.FaceDown))
		return javaName(below) == "pitcher_crop" && javaString(below, "half") == "lower"
	}
	return c.tag(c.at(pos.Side(cube.FaceDown)), "supports_crops")
}

func javaPlant06DoublePlantShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	half := javaString(state, "half")
	if face.Axis() != cube.Y || (half == "lower") != (face == cube.FaceUp) || (javaName(neighbor) == "pitcher_crop" && javaString(neighbor, "half") != half) {
		if half == "lower" && face == cube.FaceDown && !javaPlant06PitcherCropSurvival(c, state, c.pos) {
			return javaDefault("air")
		}
		if !javaPlant06PitcherCropSurvival(c, state, c.pos) {
			return javaDefault("air")
		}
		return c.ownerShape("BlockBehaviour", state, face, neighbor)
	}
	return javaDefault("air")
}

func javaPlant06SeaPickleShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if !javaPlant06SeaPickleSurvival(c, state, c.pos) {
		return javaDefault("air")
	}
	if javaBool(state, "waterlogged") {
		c.waterTick(state, c.pos)
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func javaPlant06SeaPickleSurvival(c *javaShapeContext, _ uint32, pos cube.Pos) bool {
	below := c.at(pos.Side(cube.FaceDown))
	if javaGenerationShape(below).Sturdy&(1<<uint(cube.FaceUp)) != 0 {
		return true
	}
	nonEmpty, err := gamedata.GenerationCollisionFaceNonEmptyID(below, int(cube.FaceUp))
	if err != nil {
		panic(err)
	}
	return nonEmpty
}
