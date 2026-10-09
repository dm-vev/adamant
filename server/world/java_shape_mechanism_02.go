package world

import "github.com/df-mc/dragonfly/server/block/cube"

func init() {
	registerJavaShape("ComparatorBlock", javaMechanism02ComparatorShape)
	registerJavaShape("ConcretePowderBlock", javaMechanism02ConcretePowderShape)
	registerJavaShape("CreakingHeartBlock", javaMechanism02CreakingHeartShape)
	registerJavaShape("DriedGhastBlock", javaMechanism02DriedGhastShape)
	registerJavaShape("FallingBlock", javaMechanism02FallingShape)
	registerJavaSurvival("DiodeBlock", javaMechanism02DiodeSurvives)
}

func javaMechanism02DiodeSurvives(c *javaShapeContext, _ uint32, pos cube.Pos) bool {
	return c.sturdy(c.at(pos.Side(cube.FaceDown)), cube.FaceUp, javaSupportRigid)
}

func javaMechanism02ComparatorShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if face == cube.FaceDown && !c.sturdy(neighbor, cube.FaceUp, javaSupportRigid) {
		return javaDefault("air")
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func javaMechanism02ConcretePowderShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if javaMechanism02TouchesWater(c) {
		return javaMechanism02ConcreteForPowder(state)
	}
	return c.ownerShape("FallingBlock", state, face, neighbor)
}

func javaMechanism02ConcreteForPowder(state uint32) uint32 {
	// Concrete powder variants are a one-to-one registry family; preserve the color using
	// the canonical block-state names rather than relying on an approximate prefix match.
	name := javaName(state)
	switch name {
	case "white_concrete_powder":
		return javaDefault("white_concrete")
	case "orange_concrete_powder":
		return javaDefault("orange_concrete")
	case "magenta_concrete_powder":
		return javaDefault("magenta_concrete")
	case "light_blue_concrete_powder":
		return javaDefault("light_blue_concrete")
	case "yellow_concrete_powder":
		return javaDefault("yellow_concrete")
	case "lime_concrete_powder":
		return javaDefault("lime_concrete")
	case "pink_concrete_powder":
		return javaDefault("pink_concrete")
	case "gray_concrete_powder":
		return javaDefault("gray_concrete")
	case "light_gray_concrete_powder":
		return javaDefault("light_gray_concrete")
	case "cyan_concrete_powder":
		return javaDefault("cyan_concrete")
	case "purple_concrete_powder":
		return javaDefault("purple_concrete")
	case "blue_concrete_powder":
		return javaDefault("blue_concrete")
	case "brown_concrete_powder":
		return javaDefault("brown_concrete")
	case "green_concrete_powder":
		return javaDefault("green_concrete")
	case "red_concrete_powder":
		return javaDefault("red_concrete")
	case "black_concrete_powder":
		return javaDefault("black_concrete")
	default:
		panic("unexpected ConcretePowderBlock state " + name)
	}
}

func javaMechanism02TouchesWater(c *javaShapeContext) bool {
	centerIsWater := c.fluid(c.at(c.pos)).kind == javaFluidWater
	for _, direction := range [...]cube.Face{cube.FaceDown, cube.FaceUp, cube.FaceNorth, cube.FaceSouth, cube.FaceWest, cube.FaceEast} {
		if direction == cube.FaceDown && !centerIsWater {
			continue
		}
		neighbor := c.at(c.pos.Side(direction))
		if c.fluid(neighbor).kind == javaFluidWater && !c.sturdy(neighbor, direction.Opposite(), javaSupportFull) {
			return true
		}
	}
	return false
}

func javaMechanism02CreakingHeartShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	c.blockTick(c.pos, state, 1)
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func javaMechanism02DriedGhastShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if javaBool(state, "waterlogged") {
		c.fluidTick(c.pos, javaFluidState{kind: javaFluidWater, amount: 8, source: true}, 5)
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func javaMechanism02FallingShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	delay := int32(2)
	if javaName(state) == "dragon_egg" {
		delay = 5
	}
	c.blockTick(c.pos, state, delay)
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}
