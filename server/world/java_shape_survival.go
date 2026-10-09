package world

import "github.com/df-mc/dragonfly/server/block/cube"

// These classes inherit an updateShape implementation which invokes their
// own canSurvive override. They therefore do not appear as shape owners.
func init() {
	registerJavaSurvival("CakeBlock", javaCakeSurvives)
	registerJavaSurvival("CropBlock", javaCropSurvives)
	registerJavaSurvival("GrindstoneBlock", javaGrindstoneSurvives)
	registerJavaSurvival("LeafLitterBlock", javaLeafLitterSurvives)
	registerJavaSurvival("MushroomBlock", javaMushroomSurvives)
	registerJavaSurvival("TallSeagrassBlock", javaTallSeagrassSurvives)
}

func javaCakeSurvives(c *javaShapeContext, state uint32, pos cube.Pos) bool {
	return c.solid(c.at(pos.Side(cube.FaceDown)))
}

func javaCropSurvives(c *javaShapeContext, state uint32, pos cube.Pos) bool {
	return c.light(pos) >= 8 && c.tag(c.at(pos.Side(cube.FaceDown)), "supports_crops")
}

func javaGrindstoneSurvives(c *javaShapeContext, state uint32, pos cube.Pos) bool {
	// GrindstoneBlock explicitly overrides the face-attached support rule.
	return true
}

func javaLeafLitterSurvives(c *javaShapeContext, state uint32, pos cube.Pos) bool {
	return c.sturdy(c.at(pos.Side(cube.FaceDown)), cube.FaceUp, javaSupportFull)
}

func javaMushroomSurvives(c *javaShapeContext, state uint32, pos cube.Pos) bool {
	below := c.at(pos.Side(cube.FaceDown))
	return c.tag(below, "overrides_mushroom_light_requirement") || c.light(pos) < 13 && javaGenerationShape(below).SolidRender
}

func javaTallSeagrassSurvives(c *javaShapeContext, state uint32, pos cube.Pos) bool {
	below := c.at(pos.Side(cube.FaceDown))
	if javaString(state, "half") == "upper" {
		return javaName(below) == javaName(state) && javaString(below, "half") == "lower"
	}
	fluid := c.fluid(c.at(pos))
	return c.sturdy(below, cube.FaceUp, javaSupportFull) && !c.tag(below, "cannot_support_seagrass") && fluid.kind == javaFluidWater && fluid.amount == 8
}
