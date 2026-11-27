package world

import "github.com/dm-vev/adamant/server/block/cube"

func init() {
	registerShape("ComparatorBlock", mechanism02ComparatorShape)
	registerShape("ConcretePowderBlock", mechanism02ConcretePowderShape)
	registerShape("CreakingHeartBlock", mechanism02CreakingHeartShape)
	registerShape("DriedGhastBlock", mechanism02DriedGhastShape)
	registerShape("FallingBlock", mechanism02FallingShape)
	registerSurvival("DiodeBlock", mechanism02DiodeSurvives)
}

func mechanism02DiodeSurvives(c *shapeContext, _ uint32, pos cube.Pos) bool {
	return c.sturdy(c.at(pos.Side(cube.FaceDown)), cube.FaceUp, supportRigid)
}

func mechanism02ComparatorShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if face == cube.FaceDown && !c.sturdy(neighbor, cube.FaceUp, supportRigid) {
		return stateDefault("air")
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func mechanism02ConcretePowderShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if mechanism02TouchesWater(c) {
		return mechanism02ConcreteForPowder(state)
	}
	return c.ownerShape("FallingBlock", state, face, neighbor)
}

func mechanism02ConcreteForPowder(state uint32) uint32 {
	// Concrete powder variants are a one-to-one registry family; preserve the color using
	// the canonical block-state names rather than relying on an approximate prefix match.
	name := stateName(state)
	switch name {
	case "white_concrete_powder":
		return stateDefault("white_concrete")
	case "orange_concrete_powder":
		return stateDefault("orange_concrete")
	case "magenta_concrete_powder":
		return stateDefault("magenta_concrete")
	case "light_blue_concrete_powder":
		return stateDefault("light_blue_concrete")
	case "yellow_concrete_powder":
		return stateDefault("yellow_concrete")
	case "lime_concrete_powder":
		return stateDefault("lime_concrete")
	case "pink_concrete_powder":
		return stateDefault("pink_concrete")
	case "gray_concrete_powder":
		return stateDefault("gray_concrete")
	case "light_gray_concrete_powder":
		return stateDefault("light_gray_concrete")
	case "cyan_concrete_powder":
		return stateDefault("cyan_concrete")
	case "purple_concrete_powder":
		return stateDefault("purple_concrete")
	case "blue_concrete_powder":
		return stateDefault("blue_concrete")
	case "brown_concrete_powder":
		return stateDefault("brown_concrete")
	case "green_concrete_powder":
		return stateDefault("green_concrete")
	case "red_concrete_powder":
		return stateDefault("red_concrete")
	case "black_concrete_powder":
		return stateDefault("black_concrete")
	default:
		panic("unexpected ConcretePowderBlock state " + name)
	}
}

func mechanism02TouchesWater(c *shapeContext) bool {
	centerIsWater := c.fluid(c.at(c.pos)).kind == fluidWater
	for _, direction := range [...]cube.Face{cube.FaceDown, cube.FaceUp, cube.FaceNorth, cube.FaceSouth, cube.FaceWest, cube.FaceEast} {
		if direction == cube.FaceDown && !centerIsWater {
			continue
		}
		neighbor := c.at(c.pos.Side(direction))
		if c.fluid(neighbor).kind == fluidWater && !c.sturdy(neighbor, direction.Opposite(), supportFull) {
			return true
		}
	}
	return false
}

func mechanism02CreakingHeartShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	c.blockTick(c.pos, state, 1)
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func mechanism02DriedGhastShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if stateBool(state, "waterlogged") {
		c.fluidTick(c.pos, fluidState{kind: fluidWater, amount: 8, source: true}, 5)
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func mechanism02FallingShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	delay := int32(2)
	if stateName(state) == "dragon_egg" {
		delay = 5
	}
	c.blockTick(c.pos, state, delay)
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}
