package world

import "github.com/dm-vev/adamant/server/block/cube"

func init() {
	registerShape("CocoaBlock", plant03CocoaShape)
	registerShape("CoralBlock", plant03CoralShape)
	registerShape("CoralFanBlock", plant03CoralFanShape)
	registerShape("CoralPlantBlock", plant03CoralPlantShape)
	registerShape("CoralWallFanBlock", plant03CoralWallFanShape)
	registerSurvival("CocoaBlock", plant03CocoaSurvives)

}

func plant03CocoaShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if face == plant03Direction(stateString(state, "facing")) && !c.canSurvive(state, c.pos) {
		return stateDefault("air")
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func plant03CocoaSurvives(c *shapeContext, state uint32, pos cube.Pos) bool {
	support := c.at(pos.Side(plant03Direction(stateString(state, "facing"))))
	return c.tag(support, "minecraft:supports_cocoa")
}

func plant03CoralShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if !plant03CoralHasWater(c, c.pos) {
		c.blockTick(c.pos, state, 60+c.randomInt(40))
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func plant03CoralFanShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if face == cube.FaceDown && !c.canSurvive(state, c.pos) {
		return stateDefault("air")
	}
	plant03ScheduleCoralDeath(c, state, c.pos)
	if stateBool(state, "waterlogged") {
		c.waterTick(state, c.pos)
	}
	return c.ownerShape("BaseCoralPlantTypeBlock", state, face, neighbor)
}

func plant03CoralPlantShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if face == cube.FaceDown && !c.canSurvive(state, c.pos) {
		return stateDefault("air")
	}
	plant03ScheduleCoralDeath(c, state, c.pos)
	if stateBool(state, "waterlogged") {
		c.waterTick(state, c.pos)
	}
	return c.ownerShape("BaseCoralPlantTypeBlock", state, face, neighbor)
}

func plant03CoralWallFanShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	facing := plant03Direction(stateString(state, "facing"))
	if face.Opposite() == facing && !c.canSurvive(state, c.pos) {
		return stateDefault("air")
	}
	if stateBool(state, "waterlogged") {
		c.waterTick(state, c.pos)
	}
	plant03ScheduleCoralDeath(c, state, c.pos)
	return c.ownerShape("BaseCoralWallFanBlock", state, face, neighbor)
}

func plant03ScheduleCoralDeath(c *shapeContext, state uint32, pos cube.Pos) {
	if !stateBool(state, "waterlogged") && !plant03CoralHasWater(c, pos) {
		c.blockTick(pos, state, 60+c.randomInt(40))
	}
}

func plant03CoralHasWater(c *shapeContext, pos cube.Pos) bool {
	for _, face := range [...]cube.Face{cube.FaceDown, cube.FaceUp, cube.FaceNorth, cube.FaceSouth, cube.FaceWest, cube.FaceEast} {
		if c.fluid(c.at(pos.Side(face))).kind == fluidWater {
			return true
		}
	}
	return false
}

func plant03Direction(direction string) cube.Face {
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
