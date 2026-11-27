package world

import (
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/internal/gamedata"
)

func init() {
	registerShape("SugarCaneBlock", plant08SugarCaneShape)
	registerShape("VegetationBlock", plant08VegetationShape)
	registerShape("VineBlock", plant08VineShape)
	registerSurvival("SugarCaneBlock", plant08SugarCaneSurvives)
	registerSurvival("VegetationBlock", plant08VegetationSurvives)
	registerSurvival("VineBlock", plant08VineSurvives)
}

func plant08SugarCaneShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if !plant08SugarCaneSurvives(c, state, c.pos) {
		c.blockTick(c.pos, state, 1)
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func plant08SugarCaneSurvives(c *shapeContext, _ uint32, pos cube.Pos) bool {
	belowPos := pos.Side(cube.FaceDown)
	below := c.at(belowPos)
	if stateName(below) == "sugar_cane" {
		return true
	}
	if !c.tag(below, "supports_sugar_cane") {
		return false
	}
	for _, face := range [...]cube.Face{cube.FaceNorth, cube.FaceEast, cube.FaceSouth, cube.FaceWest} {
		adjacent := belowPos.Side(face)
		block := c.at(adjacent)
		fluid := c.fluid(block)
		if fluid.kind == fluidWater || c.tag(block, "supports_sugar_cane_adjacently") {
			return true
		}
	}
	return false
}

func plant08VegetationShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if !c.canSurvive(state, c.pos) {
		return stateDefault("air")
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func plant08VegetationSurvives(c *shapeContext, state uint32, pos cube.Pos) bool {
	belowPos := pos.Side(cube.FaceDown)
	below := c.at(belowPos)
	switch stateName(state) {
	case "attached_pumpkin_stem", "pumpkin_stem":
		return c.tag(below, "supports_pumpkin_stem")
	case "attached_melon_stem", "melon_stem":
		return c.tag(below, "supports_melon_stem")
	case "azalea", "flowering_azalea":
		return c.tag(below, "supports_azalea")
	case "short_dry_grass", "tall_dry_grass", "dead_bush":
		return c.tag(below, "supports_dry_vegetation")
	case "warped_roots":
		return c.tag(below, "supports_warped_roots")
	case "crimson_roots":
		return c.tag(below, "supports_crimson_roots")
	case "nether_sprouts":
		return c.tag(below, "supports_nether_sprouts")
	case "nether_wart":
		return c.tag(below, "supports_nether_wart")
	case "lily_pad":
		above := c.fluid(c.at(belowPos.Side(cube.FaceUp)))
		return (c.fluid(below).kind == fluidWater || c.tag(below, "supports_lily_pad")) && above.kind == fluidEmpty
	case "seagrass", "tall_seagrass":
		return c.sturdy(below, cube.FaceUp, supportFull) && !c.tag(below, "cannot_support_seagrass")
	case "mangrove_propagule":
		return c.tag(below, "supports_mangrove_propagule")
	case "wither_rose":
		return c.tag(below, "supports_wither_rose")
	case "crimson_fungus":
		return c.tag(below, "supports_crimson_fungus")
	case "warped_fungus":
		return c.tag(below, "supports_warped_fungus")
	case "cactus_flower":
		return c.tag(below, "support_override_cactus_flower") || c.sturdy(below, cube.FaceUp, supportCenter)
	default:
		return c.tag(below, "supports_vegetation")
	}
}

func plant08VineShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if face == cube.FaceDown {
		return c.ownerShape("BlockBehaviour", state, face, neighbor)
	}
	updated := plant08VineUpdatedState(c, state, c.pos)
	if plant08VineFaceCount(updated) == 0 {
		return stateDefault("air")
	}
	return updated
}

func plant08VineSurvives(c *shapeContext, state uint32, pos cube.Pos) bool {
	return plant08VineFaceCount(plant08VineUpdatedState(c, state, pos)) > 0
}

func plant08VineUpdatedState(c *shapeContext, state uint32, pos cube.Pos) uint32 {
	abovePos := pos.Side(cube.FaceUp)
	if stateBool(state, "up") {
		state = stateWith(state, "up", plant08VineAcceptable(c, abovePos, cube.FaceUp))
	}
	var above uint32
	aboveRead := false
	for _, face := range [...]cube.Face{cube.FaceNorth, cube.FaceEast, cube.FaceSouth, cube.FaceWest} {
		property := plant08VineProperty(face)
		if !stateBool(state, property) {
			continue
		}
		canSupport := plant08VineCanSupport(c, pos, face)
		if !canSupport {
			if !aboveRead {
				above = c.at(abovePos)
				aboveRead = true
			}
			canSupport = stateName(above) == "vine" && stateBool(above, property)
		}
		state = stateWith(state, property, canSupport)
	}
	return state
}

func plant08VineCanSupport(c *shapeContext, pos cube.Pos, face cube.Face) bool {
	if plant08VineAcceptable(c, pos.Side(face), plant08Opposite(face)) {
		return true
	}
	if face == cube.FaceDown || face == cube.FaceUp {
		return false
	}
	above := c.at(pos.Side(cube.FaceUp))
	property := plant08VineProperty(face)
	return stateName(above) == "vine" && stateBool(above, property)
}

func plant08VineAcceptable(c *shapeContext, neighbourPos cube.Pos, supportFace cube.Face) bool {
	state := c.at(neighbourPos)
	if canonicalGenerationShape(state).Attachment&(1<<uint(supportFace)) != 0 {
		return true
	}
	full, err := gamedata.GenerationCollisionFaceCoversRectangleID(state, int(supportFace), 0, 0, 1, 1)
	if err != nil {
		panic(err)
	}
	return full
}

func plant08VineFaceCount(state uint32) int {
	count := 0
	for _, property := range [...]string{"up", "north", "east", "south", "west"} {
		if stateBool(state, property) {
			count++
		}
	}
	return count
}

func plant08VineProperty(face cube.Face) string {
	switch face {
	case cube.FaceUp:
		return "up"
	case cube.FaceNorth:
		return "north"
	case cube.FaceEast:
		return "east"
	case cube.FaceSouth:
		return "south"
	case cube.FaceWest:
		return "west"
	default:
		panic("invalid vine face")
	}
}

func plant08Opposite(face cube.Face) cube.Face {
	switch face {
	case cube.FaceDown:
		return cube.FaceUp
	case cube.FaceUp:
		return cube.FaceDown
	case cube.FaceNorth:
		return cube.FaceSouth
	case cube.FaceSouth:
		return cube.FaceNorth
	case cube.FaceWest:
		return cube.FaceEast
	case cube.FaceEast:
		return cube.FaceWest
	default:
		panic("invalid cube face")
	}
}
