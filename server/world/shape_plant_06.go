package world

import (
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/internal/gamedata"
)

func init() {
	registerShape("MangroveRootsBlock", plant06MangroveRootsShape)
	registerShape("MossyCarpetBlock", plant06MossyCarpetShape)
	registerShape("MultifaceBlock", plant06MultifaceShape)
	registerShape("PitcherCropBlock", plant06PitcherCropShape)
	registerShape("SeaPickleBlock", plant06SeaPickleShape)

	registerSurvival("MossyCarpetBlock", plant06MossyCarpetSurvival)
	registerSurvival("MultifaceBlock", plant06MultifaceSurvival)
	registerSurvival("PitcherCropBlock", plant06PitcherCropSurvival)
	registerSurvival("SeaPickleBlock", plant06SeaPickleSurvival)
}

func plant06MangroveRootsShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if stateBool(state, "waterlogged") {
		c.waterTick(state, c.pos)
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func plant06MossyCarpetShape(c *shapeContext, state uint32, _ cube.Face, _ uint32) uint32 {
	if !plant06MossyCarpetSurvival(c, state, c.pos) {
		return stateDefault("air")
	}
	updated := plant06UpdateMossyCarpetSides(c, state, false)
	if !plant06MossyCarpetHasFaces(updated) {
		return stateDefault("air")
	}
	return updated
}

func plant06MossyCarpetSurvival(c *shapeContext, state uint32, pos cube.Pos) bool {
	below := c.at(pos.Side(cube.FaceDown))
	if stateBool(state, "bottom") {
		return !plant06IsAir(below)
	}
	return stateName(below) == "pale_moss_carpet" && stateBool(below, "bottom")
}

func plant06IsAir(state uint32) bool {
	switch stateName(state) {
	case "air", "cave_air", "void_air":
		return true
	default:
		return false
	}
}

func plant06MossyCarpetHasFaces(state uint32) bool {
	if stateBool(state, "bottom") {
		return true
	}
	for _, property := range [...]string{"north", "east", "south", "west"} {
		if stateString(state, property) != "none" {
			return true
		}
	}
	return false
}

func plant06UpdateMossyCarpetSides(c *shapeContext, state uint32, createSides bool) uint32 {
	createSides = createSides || stateBool(state, "bottom")
	var above, below uint32
	aboveRead, belowRead := false, false
	for _, item := range [...]struct {
		face     cube.Face
		property string
	}{{cube.FaceNorth, "north"}, {cube.FaceEast, "east"}, {cube.FaceSouth, "south"}, {cube.FaceWest, "west"}} {
		side := stateString(state, item.property)
		if plant06CanSupportCarpetFace(c, item.face) {
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
			if stateName(above) == "pale_moss_carpet" && stateString(above, item.property) != "none" && !stateBool(above, "bottom") {
				side = "tall"
			}
			if !stateBool(state, "bottom") {
				if !belowRead {
					below = c.at(c.pos.Side(cube.FaceDown))
					belowRead = true
				}
				if stateName(below) == "pale_moss_carpet" && stateString(below, item.property) == "none" {
					side = "none"
				}
			}
		}
		state = stateWith(state, item.property, side)
	}
	return state
}

func plant06CanSupportCarpetFace(c *shapeContext, direction cube.Face) bool {
	if direction == cube.FaceUp {
		return false
	}
	neighbor := c.at(c.pos.Side(direction))
	return plant06FaceFull(neighbor, direction.Opposite())
}

func plant06MultifaceShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if stateBool(state, "waterlogged") {
		c.waterTick(state, c.pos)
	}
	if !plant06MultifaceHasAnyFace(state) {
		return stateDefault("air")
	}
	if stateBool(state, plant06FaceProperty(face)) && !plant06CanAttachTo(c, c.pos.Side(face), face.Opposite(), neighbor) {
		updated := stateWith(state, plant06FaceProperty(face), false)
		if !plant06MultifaceHasAnyFace(updated) {
			return stateDefault("air")
		}
		return updated
	}
	return state
}

func plant06MultifaceSurvival(c *shapeContext, state uint32, pos cube.Pos) bool {
	hasFace := false
	for _, face := range [...]cube.Face{cube.FaceDown, cube.FaceUp, cube.FaceNorth, cube.FaceSouth, cube.FaceWest, cube.FaceEast} {
		if stateBool(state, plant06FaceProperty(face)) {
			hasFace = true
			if !plant06CanAttachTo(c, pos.Side(face), face.Opposite(), c.at(pos.Side(face))) {
				return false
			}
		}
	}
	return hasFace
}

func plant06MultifaceHasAnyFace(state uint32) bool {
	for _, property := range [...]string{"down", "up", "north", "south", "west", "east"} {
		if stateBool(state, property) {
			return true
		}
	}
	return false
}

func plant06CanAttachTo(c *shapeContext, _ cube.Pos, supportFace cube.Face, state uint32) bool {
	return plant06FaceFull(state, supportFace)
}

func plant06FaceFull(state uint32, face cube.Face) bool {
	if canonicalGenerationShape(state).Attachment&(1<<uint(face)) != 0 {
		return true
	}
	full, err := gamedata.GenerationCollisionFaceCoversRectangleID(state, int(face), 0, 0, 1, 1)
	if err != nil {
		panic(err)
	}
	return full
}

func plant06FaceProperty(face cube.Face) string {
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

func plant06PitcherCropShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if stateInt(state, "age") >= 3 {
		return plant06DoublePlantShape(c, state, face, neighbor)
	}
	if plant06PitcherCropSurvival(c, state, c.pos) {
		return state
	}
	return stateDefault("air")
}

func plant06PitcherCropSurvival(c *shapeContext, state uint32, pos cube.Pos) bool {
	lower := stateName(state) == "pitcher_crop" && stateString(state, "half") == "lower"
	if lower && c.light(pos) < 8 {
		return false
	}
	if stateString(state, "half") == "upper" {
		below := c.at(pos.Side(cube.FaceDown))
		return stateName(below) == "pitcher_crop" && stateString(below, "half") == "lower"
	}
	return c.tag(c.at(pos.Side(cube.FaceDown)), "supports_crops")
}

func plant06DoublePlantShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	half := stateString(state, "half")
	if face.Axis() != cube.Y || (half == "lower") != (face == cube.FaceUp) || (stateName(neighbor) == "pitcher_crop" && stateString(neighbor, "half") != half) {
		if half == "lower" && face == cube.FaceDown && !plant06PitcherCropSurvival(c, state, c.pos) {
			return stateDefault("air")
		}
		if !plant06PitcherCropSurvival(c, state, c.pos) {
			return stateDefault("air")
		}
		return c.ownerShape("BlockBehaviour", state, face, neighbor)
	}
	return stateDefault("air")
}

func plant06SeaPickleShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if !plant06SeaPickleSurvival(c, state, c.pos) {
		return stateDefault("air")
	}
	if stateBool(state, "waterlogged") {
		c.waterTick(state, c.pos)
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func plant06SeaPickleSurvival(c *shapeContext, _ uint32, pos cube.Pos) bool {
	below := c.at(pos.Side(cube.FaceDown))
	if canonicalGenerationShape(below).Sturdy&(1<<uint(cube.FaceUp)) != 0 {
		return true
	}
	nonEmpty, err := gamedata.GenerationCollisionFaceNonEmptyID(below, int(cube.FaceUp))
	if err != nil {
		panic(err)
	}
	return nonEmpty
}
