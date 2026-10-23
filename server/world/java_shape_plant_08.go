package world

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
)

func init() {
	registerJavaShape("SugarCaneBlock", javaPlant08SugarCaneShape)
	registerJavaShape("VegetationBlock", javaPlant08VegetationShape)
	registerJavaShape("VineBlock", javaPlant08VineShape)
	registerJavaSurvival("SugarCaneBlock", javaPlant08SugarCaneSurvives)
	registerJavaSurvival("VegetationBlock", javaPlant08VegetationSurvives)
	registerJavaSurvival("VineBlock", javaPlant08VineSurvives)
}

func javaPlant08SugarCaneShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if !javaPlant08SugarCaneSurvives(c, state, c.pos) {
		c.blockTick(c.pos, state, 1)
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func javaPlant08SugarCaneSurvives(c *javaShapeContext, _ uint32, pos cube.Pos) bool {
	belowPos := pos.Side(cube.FaceDown)
	below := c.at(belowPos)
	if javaName(below) == "sugar_cane" {
		return true
	}
	if !c.tag(below, "supports_sugar_cane") {
		return false
	}
	for _, face := range [...]cube.Face{cube.FaceNorth, cube.FaceEast, cube.FaceSouth, cube.FaceWest} {
		adjacent := belowPos.Side(face)
		block := c.at(adjacent)
		fluid := c.fluid(block)
		if fluid.kind == javaFluidWater || c.tag(block, "supports_sugar_cane_adjacently") {
			return true
		}
	}
	return false
}

func javaPlant08VegetationShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if !c.canSurvive(state, c.pos) {
		return javaDefault("air")
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func javaPlant08VegetationSurvives(c *javaShapeContext, state uint32, pos cube.Pos) bool {
	belowPos := pos.Side(cube.FaceDown)
	below := c.at(belowPos)
	switch javaName(state) {
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
		return (c.fluid(below).kind == javaFluidWater || c.tag(below, "supports_lily_pad")) && above.kind == javaFluidEmpty
	case "seagrass", "tall_seagrass":
		return c.sturdy(below, cube.FaceUp, javaSupportFull) && !c.tag(below, "cannot_support_seagrass")
	case "mangrove_propagule":
		return c.tag(below, "supports_mangrove_propagule")
	case "wither_rose":
		return c.tag(below, "supports_wither_rose")
	case "crimson_fungus":
		return c.tag(below, "supports_crimson_fungus")
	case "warped_fungus":
		return c.tag(below, "supports_warped_fungus")
	case "cactus_flower":
		return c.tag(below, "support_override_cactus_flower") || c.sturdy(below, cube.FaceUp, javaSupportCenter)
	default:
		return c.tag(below, "supports_vegetation")
	}
}

func javaPlant08VineShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if face == cube.FaceDown {
		return c.ownerShape("BlockBehaviour", state, face, neighbor)
	}
	updated := javaPlant08VineUpdatedState(c, state, c.pos)
	if javaPlant08VineFaceCount(updated) == 0 {
		return javaDefault("air")
	}
	return updated
}

func javaPlant08VineSurvives(c *javaShapeContext, state uint32, pos cube.Pos) bool {
	return javaPlant08VineFaceCount(javaPlant08VineUpdatedState(c, state, pos)) > 0
}

func javaPlant08VineUpdatedState(c *javaShapeContext, state uint32, pos cube.Pos) uint32 {
	abovePos := pos.Side(cube.FaceUp)
	if javaBool(state, "up") {
		state = javaWith(state, "up", javaPlant08VineAcceptable(c, abovePos, cube.FaceUp))
	}
	var above uint32
	aboveRead := false
	for _, face := range [...]cube.Face{cube.FaceNorth, cube.FaceEast, cube.FaceSouth, cube.FaceWest} {
		property := javaPlant08VineProperty(face)
		if !javaBool(state, property) {
			continue
		}
		canSupport := javaPlant08VineCanSupport(c, pos, face)
		if !canSupport {
			if !aboveRead {
				above = c.at(abovePos)
				aboveRead = true
			}
			canSupport = javaName(above) == "vine" && javaBool(above, property)
		}
		state = javaWith(state, property, canSupport)
	}
	return state
}

func javaPlant08VineCanSupport(c *javaShapeContext, pos cube.Pos, face cube.Face) bool {
	if javaPlant08VineAcceptable(c, pos.Side(face), javaPlant08Opposite(face)) {
		return true
	}
	if face == cube.FaceDown || face == cube.FaceUp {
		return false
	}
	above := c.at(pos.Side(cube.FaceUp))
	property := javaPlant08VineProperty(face)
	return javaName(above) == "vine" && javaBool(above, property)
}

func javaPlant08VineAcceptable(c *javaShapeContext, neighbourPos cube.Pos, supportFace cube.Face) bool {
	state := c.at(neighbourPos)
	if javaGenerationShape(state).Attachment&(1<<uint(supportFace)) != 0 {
		return true
	}
	full, err := gamedata.GenerationCollisionFaceCoversRectangleID(state, int(supportFace), 0, 0, 1, 1)
	if err != nil {
		panic(err)
	}
	return full
}

func javaPlant08VineFaceCount(state uint32) int {
	count := 0
	for _, property := range [...]string{"up", "north", "east", "south", "west"} {
		if javaBool(state, property) {
			count++
		}
	}
	return count
}

func javaPlant08VineProperty(face cube.Face) string {
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

func javaPlant08Opposite(face cube.Face) cube.Face {
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
