package world

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
)

func init() {
	registerJavaShape("HangingMossBlock", javaPlant05HangingMossShape)
	registerJavaShape("HangingRootsBlock", javaPlant05HangingRootsShape)
	registerJavaShape("HugeMushroomBlock", javaPlant05HugeMushroomShape)
	registerJavaShape("LeavesBlock", javaPlant05LeavesShape)
	registerJavaShape("MangrovePropaguleBlock", javaPlant05MangrovePropaguleShape)

	registerJavaSurvival("HangingMossBlock", javaPlant05HangingMossSurvival)
	registerJavaSurvival("HangingRootsBlock", javaPlant05HangingRootsSurvival)
	registerJavaSurvival("MangrovePropaguleBlock", javaPlant05MangrovePropaguleSurvival)
}

func javaPlant05HangingMossShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if !javaPlant05HangingMossSurvival(c, state, c.pos) {
		c.blockTick(c.pos, state, 1)
	}
	return javaWith(state, "tip", javaName(c.at(c.pos.Side(cube.FaceDown))) != javaName(state))
}

func javaPlant05HangingMossSurvival(c *javaShapeContext, state uint32, pos cube.Pos) bool {
	abovePos := pos.Side(cube.FaceUp)
	above := c.at(abovePos)
	return javaGenerationShape(above).Attachment&(1<<uint(cube.FaceDown)) != 0 || javaName(above) == javaName(state)
}

func javaPlant05HangingRootsShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if face == cube.FaceUp && !javaPlant05HangingRootsSurvival(c, state, c.pos) {
		return javaDefault("air")
	}
	if javaBool(state, "waterlogged") {
		c.waterTick(state, c.pos)
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func javaPlant05HangingRootsSurvival(c *javaShapeContext, state uint32, pos cube.Pos) bool {
	above := c.at(pos.Side(cube.FaceUp))
	return c.sturdy(above, cube.FaceDown, javaSupportFull)
}

func javaPlant05HugeMushroomShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if javaName(neighbor) == javaName(state) {
		return javaWith(state, javaPlant05FaceProperty(face), false)
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func javaPlant05FaceProperty(face cube.Face) string {
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
		panic("invalid cube face")
	}
}

func javaPlant05LeavesShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if javaBool(state, "waterlogged") {
		c.waterTick(state, c.pos)
	}
	distance, hasDistance := gamedata.BlockStateInt(neighbor, "distance")
	if c.tag(neighbor, "prevents_nearby_leaf_decay") {
		distance = 0
	} else if !hasDistance {
		distance = 7
	}
	distance++
	current := javaInt(state, "distance")
	if distance != 1 || current != distance {
		c.blockTick(c.pos, state, 1)
	}
	return state
}

func javaPlant05MangrovePropaguleShape(c *javaShapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if javaBool(state, "waterlogged") {
		c.waterTick(state, c.pos)
	}
	if face == cube.FaceUp && !c.canSurvive(state, c.pos) {
		return javaDefault("air")
	}
	return c.ownerShape("VegetationBlock", state, face, neighbor)
}

func javaPlant05MangrovePropaguleSurvival(c *javaShapeContext, state uint32, pos cube.Pos) bool {
	if javaBool(state, "hanging") {
		return c.tag(c.at(pos.Side(cube.FaceUp)), "supports_hanging_mangrove_propagule")
	}
	return c.ownerSurvival("VegetationBlock", state, pos)
}
