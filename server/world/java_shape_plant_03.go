package world

import "github.com/df-mc/dragonfly/server/block/cube"

func init() {
	registerJavaShape("CocoaBlock", javaPlant03CocoaShape)
	registerJavaShape("CoralBlock", javaPlant03CoralShape)
	registerJavaShape("CoralFanBlock", javaPlant03CoralFanShape)
	registerJavaShape("CoralPlantBlock", javaPlant03CoralPlantShape)
	registerJavaShape("CoralWallFanBlock", javaPlant03CoralWallFanShape)
	registerJavaSurvival("CocoaBlock", javaPlant03CocoaSurvives)

}

func javaPlant03CocoaShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if face == javaPlant03Direction(javaString(state, "facing")) && !c.canSurvive(state, c.pos) {
		return javaDefault("air")
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func javaPlant03CocoaSurvives(c *javaShapeContext, state uint32, pos cube.Pos) bool {
	support := c.at(pos.Side(javaPlant03Direction(javaString(state, "facing"))))
	return c.tag(support, "minecraft:supports_cocoa")
}

func javaPlant03CoralShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if !javaPlant03CoralHasWater(c, c.pos) {
		c.blockTick(c.pos, state, 60+c.randomInt(40))
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func javaPlant03CoralFanShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if face == cube.FaceDown && !c.canSurvive(state, c.pos) {
		return javaDefault("air")
	}
	javaPlant03ScheduleCoralDeath(c, state, c.pos)
	if javaBool(state, "waterlogged") {
		c.waterTick(state, c.pos)
	}
	return c.ownerShape("BaseCoralPlantTypeBlock", state, face, neighbor)
}

func javaPlant03CoralPlantShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if face == cube.FaceDown && !c.canSurvive(state, c.pos) {
		return javaDefault("air")
	}
	javaPlant03ScheduleCoralDeath(c, state, c.pos)
	if javaBool(state, "waterlogged") {
		c.waterTick(state, c.pos)
	}
	return c.ownerShape("BaseCoralPlantTypeBlock", state, face, neighbor)
}

func javaPlant03CoralWallFanShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	facing := javaPlant03Direction(javaString(state, "facing"))
	if face.Opposite() == facing && !c.canSurvive(state, c.pos) {
		return javaDefault("air")
	}
	if javaBool(state, "waterlogged") {
		c.waterTick(state, c.pos)
	}
	javaPlant03ScheduleCoralDeath(c, state, c.pos)
	return c.ownerShape("BaseCoralWallFanBlock", state, face, neighbor)
}

func javaPlant03ScheduleCoralDeath(c *javaShapeContext, state uint32, pos cube.Pos) {
	if !javaBool(state, "waterlogged") && !javaPlant03CoralHasWater(c, pos) {
		c.blockTick(pos, state, 60+c.randomInt(40))
	}
}

func javaPlant03CoralHasWater(c *javaShapeContext, pos cube.Pos) bool {
	for _, face := range [...]cube.Face{cube.FaceDown, cube.FaceUp, cube.FaceNorth, cube.FaceSouth, cube.FaceWest, cube.FaceEast} {
		if c.fluid(c.at(pos.Side(face))).kind == javaFluidWater {
			return true
		}
	}
	return false
}

func javaPlant03Direction(direction string) cube.Face {
	switch direction {
	case "down":
		return cube.FaceDown
	case "up":
		return cube.FaceUp
	case "north":
		return cube.FaceNorth
	case "south":
		return cube.FaceSouth
	case "west":
		return cube.FaceWest
	case "east":
		return cube.FaceEast
	default:
		panic("invalid Java plant direction " + direction)
	}
}
