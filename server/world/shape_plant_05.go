package world

import (
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/internal/gamedata"
)

func init() {
	registerShape("HangingMossBlock", plant05HangingMossShape)
	registerShape("HangingRootsBlock", plant05HangingRootsShape)
	registerShape("HugeMushroomBlock", plant05HugeMushroomShape)
	registerShape("LeavesBlock", plant05LeavesShape)
	registerShape("MangrovePropaguleBlock", plant05MangrovePropaguleShape)

	registerSurvival("HangingMossBlock", plant05HangingMossSurvival)
	registerSurvival("HangingRootsBlock", plant05HangingRootsSurvival)
	registerSurvival("MangrovePropaguleBlock", plant05MangrovePropaguleSurvival)
}

func plant05HangingMossShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if !plant05HangingMossSurvival(c, state, c.pos) {
		c.blockTick(c.pos, state, 1)
	}
	return stateWith(state, "tip", stateName(c.at(c.pos.Side(cube.FaceDown))) != stateName(state))
}

func plant05HangingMossSurvival(c *shapeContext, state uint32, pos cube.Pos) bool {
	abovePos := pos.Side(cube.FaceUp)
	above := c.at(abovePos)
	return canonicalGenerationShape(above).Attachment&(1<<uint(cube.FaceDown)) != 0 || stateName(above) == stateName(state)
}

func plant05HangingRootsShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if face == cube.FaceUp && !plant05HangingRootsSurvival(c, state, c.pos) {
		return stateDefault("air")
	}
	if stateBool(state, "waterlogged") {
		c.waterTick(state, c.pos)
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func plant05HangingRootsSurvival(c *shapeContext, state uint32, pos cube.Pos) bool {
	above := c.at(pos.Side(cube.FaceUp))
	return c.sturdy(above, cube.FaceDown, supportFull)
}

func plant05HugeMushroomShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if stateName(neighbor) == stateName(state) {
		return stateWith(state, plant05FaceProperty(face), false)
	}
	return c.ownerShape("BlockBehaviour", state, face, neighbor)
}

func plant05FaceProperty(face cube.Face) string {
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

func plant05LeavesShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if stateBool(state, "waterlogged") {
		c.waterTick(state, c.pos)
	}
	distance, hasDistance := gamedata.BlockStateInt(neighbor, "distance")
	if c.tag(neighbor, "prevents_nearby_leaf_decay") {
		distance = 0
	} else if !hasDistance {
		distance = 7
	}
	distance++
	current := stateInt(state, "distance")
	if distance != 1 || current != distance {
		c.blockTick(c.pos, state, 1)
	}
	return state
}

func plant05MangrovePropaguleShape(c *shapeContext, state uint32, face cube.Face, neighbor uint32) uint32 {
	if stateBool(state, "waterlogged") {
		c.waterTick(state, c.pos)
	}
	if face == cube.FaceUp && !c.canSurvive(state, c.pos) {
		return stateDefault("air")
	}
	return c.ownerShape("VegetationBlock", state, face, neighbor)
}

func plant05MangrovePropaguleSurvival(c *shapeContext, state uint32, pos cube.Pos) bool {
	if stateBool(state, "hanging") {
		return c.tag(c.at(pos.Side(cube.FaceUp)), "supports_hanging_mangrove_propagule")
	}
	return c.ownerSurvival("VegetationBlock", state, pos)
}
