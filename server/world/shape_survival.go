package world

import "github.com/dm-vev/adamant/server/block/cube"

// These classes inherit an updateShape implementation which invokes their
// own canSurvive override. They therefore do not appear as shape owners.
func init() {
	registerSurvival("CakeBlock", cakeSurvives)
	registerSurvival("CropBlock", cropSurvives)
	registerSurvival("GrindstoneBlock", grindstoneSurvives)
	registerSurvival("LeafLitterBlock", leafLitterSurvives)
	registerSurvival("MushroomBlock", mushroomSurvives)
	registerSurvival("TallSeagrassBlock", tallSeagrassSurvives)
}

func cakeSurvives(c *shapeContext, state uint32, pos cube.Pos) bool {
	return c.solid(c.at(pos.Side(cube.FaceDown)))
}

func cropSurvives(c *shapeContext, state uint32, pos cube.Pos) bool {
	return c.light(pos) >= 8 && c.tag(c.at(pos.Side(cube.FaceDown)), "supports_crops")
}

func grindstoneSurvives(c *shapeContext, state uint32, pos cube.Pos) bool {
	// GrindstoneBlock explicitly overrides the face-attached support rule.
	return true
}

func leafLitterSurvives(c *shapeContext, state uint32, pos cube.Pos) bool {
	return c.sturdy(c.at(pos.Side(cube.FaceDown)), cube.FaceUp, supportFull)
}

func mushroomSurvives(c *shapeContext, state uint32, pos cube.Pos) bool {
	below := c.at(pos.Side(cube.FaceDown))
	return c.tag(below, "overrides_mushroom_light_requirement") || c.light(pos) < 13 && canonicalGenerationShape(below).SolidRender
}

func tallSeagrassSurvives(c *shapeContext, state uint32, pos cube.Pos) bool {
	below := c.at(pos.Side(cube.FaceDown))
	if stateString(state, "half") == "upper" {
		return stateName(below) == stateName(state) && stateString(below, "half") == "lower"
	}
	fluid := c.fluid(c.at(pos))
	return c.sturdy(below, cube.FaceUp, supportFull) && !c.tag(below, "cannot_support_seagrass") && fluid.kind == fluidWater && fluid.amount == 8
}
